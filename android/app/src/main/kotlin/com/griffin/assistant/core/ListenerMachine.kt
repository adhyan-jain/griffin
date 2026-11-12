package com.griffin.assistant.core

/**
 * The speech listener's decisions, as ONE pure function ([ListenerMachine.reduce]). Before this they were spread over
 * seven fields (active, paused, restartPending, forceListenNext, an error counter and two engine-preference flags)
 * that every callback had to keep in step by hand. One of those paths (stop while a restart was pending) left
 * `restartPending` set, which silently blocked every later restart.
 *
 *   Stopped ──Start──▶ Running ──(result, silence, error)──▶ Restart pending ──timer──▶ Running
 *                         │  ▲
 *                      Pause│Resume            ForceListen = cancel everything and listen once, now
 *                         ▼  │
 *                       Paused (the assistant is speaking)
 *
 * The shell ([com.griffin.assistant.Listener]) owns the Android recogniser and the clock. It feeds events in and
 * runs the returned [LEffect]s, so this file has no Android imports and is tested on a laptop.
 */
data class LState(
    /** Wanted to be listening (between Start and Stop, or until a fatal error). */
    val active: Boolean = false,
    /** The assistant is speaking; do not hear it. */
    val paused: Boolean = false,
    /** A restart timer is scheduled. At most one at a time. */
    val restartPending: Boolean = false,
    /** The next listen goes ahead even while media is playing. */
    val forceNext: Boolean = false,
    val clientErrors: Int = 0,
    /** Try the private on-device engine first; cleared when it fails. */
    val preferOnDevice: Boolean = true,
    val preferOffline: Boolean = false,
)

/** Why the recogniser stopped, with Android's numeric codes mapped once, in the shell. */
enum class LError { Silence, Busy, Permission, Language, Client, Network, Audio, Server, Other }

sealed class LEvent {
    object Start : LEvent()
    object Stop : LEvent()
    object Pause : LEvent()
    object Resume : LEvent()
    object ForceListen : LEvent()
    /** The restart timer fired. */
    data class TimerFired(val recreate: Boolean) : LEvent()
    /** The shell is about to start the recogniser. [mediaActive] is whether music or video is playing. */
    data class BeginAttempt(val mediaActive: Boolean) : LEvent()
    /** The shell could not create the recogniser. */
    object NoRecognizer : LEvent()
    /** startListening threw. */
    data class StartFailed(val reason: String) : LEvent()
    object Ready : LEvent()
    object SpeechBegan : LEvent()
    data class Results(val texts: List<String>) : LEvent()
    data class Error(val kind: LError, val onDeviceUsable: Boolean) : LEvent()
}

sealed class LEffect {
    /** Cancel every scheduled timer. */
    object CancelTimers : LEffect()
    /** Stop the recogniser's current listen but keep the recogniser. */
    object CancelListen : LEffect()
    /** Throw the recogniser away; the next listen builds a new one. */
    object Destroy : LEffect()
    /** Run [LEvent.BeginAttempt] now. */
    object BeginNow : LEffect()
    data class Schedule(val delayMs: Long, val recreate: Boolean) : LEffect()
    /** Create the recogniser if needed and call startListening. */
    object StartRecognizer : LEffect()
    data class Status(val text: String) : LEffect()
    data class Heard(val texts: List<String>) : LEffect()
}

data class LStep(val state: LState, val effects: List<LEffect> = emptyList())

object ListenerMachine {
    fun reduce(s: LState, e: LEvent): LStep = when (e) {
        LEvent.Start ->
            if (s.active) LStep(s) else LStep(s.copy(active = true, paused = false, clientErrors = 0), listOf(LEffect.BeginNow))

        // Stop clears restartPending too: its timers are cancelled, so a leftover flag would block every later restart.
        LEvent.Stop ->
            LStep(s.copy(active = false, restartPending = false), listOf(LEffect.CancelTimers, LEffect.Destroy, LEffect.Status("stopped")))

        LEvent.Pause ->
            LStep(s.copy(paused = true, restartPending = false), listOf(LEffect.CancelTimers, LEffect.CancelListen))

        LEvent.Resume ->
            if (!s.paused) LStep(s) else schedule(s.copy(paused = false), 350, false)

        LEvent.ForceListen ->
            LStep(s.copy(forceNext = true, restartPending = false, paused = false, active = true),
                listOf(LEffect.CancelTimers, LEffect.CancelListen, LEffect.BeginNow))

        is LEvent.TimerFired ->
            LStep(s.copy(restartPending = false), listOfNotNull(if (e.recreate) LEffect.Destroy else null, LEffect.BeginNow))

        is LEvent.BeginAttempt -> when {
            !s.active || s.paused -> LStep(s)
            e.mediaActive && !s.forceNext ->
                with(schedule(s, 2000, false)) { LStep(state, listOf<LEffect>(LEffect.Status("media playing: tap to speak")) + effects) }
            else -> LStep(s.copy(forceNext = false), listOf(LEffect.StartRecognizer))
        }

        LEvent.NoRecognizer ->
            with(schedule(s, 5000, true)) {
                LStep(state, listOf<LEffect>(LEffect.Status("needs speech recognition service: install or enable Google speech recognition")) + effects)
            }

        is LEvent.StartFailed ->
            with(schedule(s, 2000, true)) { LStep(state, listOf<LEffect>(LEffect.Status("could not start listening: ${e.reason}")) + effects) }

        LEvent.Ready -> LStep(s.copy(clientErrors = 0), listOf(LEffect.Status("listening")))

        LEvent.SpeechBegan -> LStep(s.copy(clientErrors = 0), listOf(LEffect.Status("hearing you")))

        is LEvent.Results -> {
            val heard = e.texts.map { it.trim() }.filter { it.isNotEmpty() }
            val next = schedule(s.copy(clientErrors = 0), 250, false)
            LStep(next.state, (if (heard.isNotEmpty()) listOf<LEffect>(LEffect.Heard(heard)) else emptyList()) + next.effects)
        }

        is LEvent.Error -> error(s, e)
    }

    private fun error(s: LState, e: LEvent.Error): LStep = when (e.kind) {
        LError.Silence -> schedule(s, 400, false)
        LError.Busy -> schedule(s, 900, true)
        LError.Permission -> LStep(s.copy(active = false), listOf(LEffect.Status("microphone permission is missing")))
        LError.Language -> withStatus("the speech language is unavailable", schedule(s, 5000, true))
        LError.Client -> {
            val n = s.clientErrors + 1
            val c = s.copy(clientErrors = n)
            when {
                c.preferOnDevice && e.onDeviceUsable ->
                    withStatus("on-device speech engine failed; falling back to standard engine", schedule(c.copy(preferOnDevice = false), 500, true))
                c.preferOffline ->
                    withStatus("offline speech engine failed; switching to online recognition", schedule(c.copy(preferOffline = false), 500, true))
                else ->
                    withStatus("speech client error 5: check Google app mic permission & offline speech pack",
                        schedule(c, (2000L * n).coerceAtMost(10_000L), true))
            }
        }
        LError.Network -> withStatus("speech network error, retrying", schedule(s, 3000, true))
        LError.Audio -> withStatus("audio recording error, retrying", schedule(s, 2000, true))
        LError.Server -> withStatus("speech server error, retrying", schedule(s, 3000, true))
        LError.Other -> withStatus("speech error, retrying", schedule(s, 2000, true))
    }

    private fun withStatus(text: String, step: LStep) = LStep(step.state, listOf<LEffect>(LEffect.Status(text)) + step.effects)

    /** Schedules a restart unless we are not wanted, paused, or one is already pending. */
    private fun schedule(s: LState, delayMs: Long, recreate: Boolean): LStep =
        if (!s.active || s.paused || s.restartPending) LStep(s)
        else LStep(s.copy(restartPending = true), listOf(LEffect.Schedule(delayMs, recreate)))
}
