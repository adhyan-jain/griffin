package com.griffin.assistant.core

/**
 * What the assistant is doing, as ONE value. Before this existed the answer was spread over four separate flags
 * (a wake deadline, a pending text, a ringing-call deadline and the animation's mode) that every code path had to
 * keep in step by hand. Now a single pure function, [AssistantMachine.reduce], decides every transition, so:
 *
 *  - the animation, the listening window and the confirmation can never disagree (they are all derived from [Phase]);
 *  - the whole behaviour is testable on a laptop, with no phone and no speech engine;
 *  - a person or an agent changing the assistant has one table to read and one place to change.
 *
 * Idle ──"hey <name>" or a button──▶ Awake ──request──▶ Thinking ──reply──▶ Speaking ──▶ Awake (follow-up window)
 *   ▲                                   │ timeout                                          │ timeout
 *   └───────────────────────────────────┴──────────────────────────────────────────────────┘
 *                                              Confirming ◀── (a text or reply waiting for "send")
 *
 * Time is passed in, never read, so tests control it.
 */
enum class Phase { Idle, Awake, Thinking, Speaking, Confirming }

/** Something the assistant asked the owner to confirm before doing it. */
data class PendingAction(val kind: Kind, val who: String, val target: String, val text: String, val via: String) {
    /** Sms and Reply are done on the phone; Server is a change Griffin asked the owner to confirm (the token is in [target]). */
    enum class Kind { Sms, Reply, Server }
}

data class AState(
    val phase: Phase = Phase.Idle,
    /** When Awake or Confirming runs out. */
    val until: Long = 0,
    /** Set exactly while Confirming. */
    val pending: PendingAction? = null,
    /** Where Speaking goes when the speech ends. */
    val resume: Phase = Phase.Idle,
    /** A call is ringing until then: "answer" and "reject" mean the call. */
    val ringingUntil: Long = 0,
)

sealed class AEvent {
    /** Something the recogniser heard. [name] is the manager's current name. */
    data class Heard(val text: String, val now: Long, val name: String) : AEvent()
    data class CallRinging(val now: Long) : AEvent()
    object SpeechStarted : AEvent()
    data class SpeechEnded(val now: Long) : AEvent()
    data class Tick(val now: Long) : AEvent()
    /** A command wants the owner's yes before it acts. */
    data class ConfirmRequested(val action: PendingAction, val now: Long) : AEvent()
    /** The owner woke it with a button, a notification action or a tile instead of saying the wake phrase. */
    data class ManualWake(val now: Long) : AEvent()
    /** The thinking ended and there is nothing to say. */
    object WorkFinished : AEvent()
}

sealed class Effect {
    /** Sentence was not for the assistant. Nothing is done with it. */
    object Ignore : Effect()
    /** The wake phrase was heard: play the tone. */
    object Beep : Effect()
    /** Do what was asked: run it as a command, or ask Griffin. */
    data class Handle(val request: String) : Effect()
    data class Confirmed(val action: PendingAction) : Effect()
    data class Declined(val action: PendingAction) : Effect()
    data class ReadDraft(val action: PendingAction) : Effect()
    data class Hold(val action: PendingAction) : Effect()
    object AnswerCall : Effect()
    object RejectCall : Effect()
}

data class Step(val state: AState, val effects: List<Effect> = emptyList())

object AssistantMachine {
    const val AWAKE_MS = 8_000L
    /** After it has spoken, it keeps listening this long for a follow-up, so a conversation needs the wake phrase once. */
    const val FOLLOWUP_MS = 7_000L
    const val CONFIRM_MS = 20_000L
    const val RINGING_MS = 25_000L

    private val yes = setOf(
        "yes", "send", "send it", "confirm", "yes send it", "yep", "yeah", "ya", "ha", "haan",
        "approve", "approve it", "approve that", "approve this", "approve task", "yes approve",
        "go ahead", "do it", "sure", "ok approve", "okay approve", "please approve", "confirm it"
    )
    private val no = setOf("no", "cancel", "don't", "do not send", "nope", "stop", "reject", "reject it", "decline")
    private val readDraft = setOf(
        "read draft", "read the draft", "read out the draft", "read out draft",
        "what is the draft", "what's the draft", "whats the draft", "tell me the draft",
        "show me the draft", "show draft", "draft", "read it", "read"
    )
    private val hold = setOf(
        "ok", "okay", "i'll check", "ill check", "i will check", "check later", "ill check later",
        "i'll check later", "leave it", "let it be", "whatever", "whatev", "later", "not now", "wait"
    )

    private fun isReadDraft(s: String): Boolean =
        s.contains("draft") || s.startsWith("read") || s.contains("what did you write") || s.contains("what does it say")

    private fun isHold(s: String): Boolean =
        s.contains("check") || s.contains("leave it") || s.contains("whatever") || s.contains("whatev") || s == "ok" || s == "okay"

    /** What the animation should show for a phase. */
    fun orb(phase: Phase): String = when (phase) {
        Phase.Idle -> "hidden"
        Phase.Awake, Phase.Confirming -> "listening"
        Phase.Thinking -> "thinking"
        Phase.Speaking -> "speaking"
    }

    fun reduce(s: AState, e: AEvent): Step = when (e) {
        is AEvent.Tick -> Step(expire(s, e.now))
        is AEvent.CallRinging -> Step(s.copy(ringingUntil = e.now + RINGING_MS))
        // Same as hearing "hey <name>" on its own, and it also cancels anything waiting for a yes.
        is AEvent.ManualWake -> Step(s.copy(phase = Phase.Awake, until = e.now + AWAKE_MS, pending = null, resume = Phase.Idle), listOf(Effect.Beep))
        is AEvent.ConfirmRequested -> Step(s.copy(phase = Phase.Confirming, until = e.now + CONFIRM_MS, pending = e.action, resume = Phase.Idle))
        AEvent.WorkFinished -> Step(if (s.phase == Phase.Thinking) s.copy(phase = Phase.Idle) else s)
        // Speech that starts while nothing is going on (a call announcement, an alarm) stays invisible.
        AEvent.SpeechStarted -> Step(if (s.phase == Phase.Idle || s.phase == Phase.Speaking) s else s.copy(phase = Phase.Speaking, resume = if (s.phase == Phase.Thinking) Phase.Awake else s.phase))
        is AEvent.SpeechEnded -> Step(if (s.phase == Phase.Speaking) back(s, e.now) else s)
        is AEvent.Heard -> heard(expire(s, e.now), e)
    }

    private fun expire(s: AState, now: Long): AState =
        if ((s.phase == Phase.Awake || s.phase == Phase.Confirming) && now >= s.until) s.copy(phase = Phase.Idle, until = 0, pending = null) else s

    private fun back(s: AState, now: Long): AState = when {
        // After a reply it listens on for a follow-up (the animation stays up while it does, so it is never a hidden mic).
        s.resume == Phase.Awake -> s.copy(phase = Phase.Awake, until = if (s.until > now) s.until else now + FOLLOWUP_MS)
        s.resume == Phase.Confirming && s.pending != null && now < s.until -> s.copy(phase = Phase.Confirming)
        else -> s.copy(phase = Phase.Idle, until = 0, pending = null, resume = Phase.Idle)
    }

    private fun heard(s0: AState, e: AEvent.Heard): Step {
        var s = s0

        // 1. A text or reply waiting for a yes. Anything else cancels the wait and is then treated as ordinary input.
        if (s.phase == Phase.Confirming && s.pending != null) {
            val p = s.pending!!
            val txt = e.text.lowercase().trim(' ', '.', '!', '?')
            when {
                txt in yes -> return Step(s.copy(phase = Phase.Thinking, until = 0, pending = null), listOf(Effect.Confirmed(p)))
                txt in no -> return Step(s.copy(phase = Phase.Thinking, until = 0, pending = null), listOf(Effect.Declined(p)))
                txt in readDraft || isReadDraft(txt) -> return Step(s.copy(phase = Phase.Confirming, until = e.now + CONFIRM_MS), listOf(Effect.ReadDraft(p)))
                txt in hold || isHold(txt) -> return Step(s.copy(phase = Phase.Idle, until = 0, pending = null), listOf(Effect.Hold(p)))
            }
            s = s.copy(phase = Phase.Idle, until = 0, pending = null)
        }

        // 2. A call that is ringing: "answer" and "reject" mean the call, with no wake phrase.
        if (e.now < s.ringingUntil) {
            when (Commands.parse(e.text)) {
                Command.Answer -> return Step(s.copy(ringingUntil = 0), listOf(Effect.AnswerCall))
                Command.Reject -> return Step(s.copy(ringingUntil = 0), listOf(Effect.RejectCall))
                else -> {}
            }
        }

        // 3. The wake phrase: "hey" and the name.
        val wake = WakeMatcher.match(e.text, e.name)
        if (wake != null) {
            // A new request cancels anything still waiting for a yes, even if its question is being spoken right now.
            val fresh = s.copy(pending = null, resume = Phase.Idle)
            return if (wake.rest.isEmpty()) Step(fresh.copy(phase = Phase.Awake, until = e.now + AWAKE_MS), listOf(Effect.Beep))
            else Step(fresh.copy(phase = Phase.Thinking, until = 0), listOf(Effect.Beep, Effect.Handle(wake.rest)))
        }

        // 4. Awake and waiting for the request: this is it.
        if (s.phase == Phase.Awake && e.now < s.until) return Step(s.copy(phase = Phase.Thinking, until = 0, pending = null, resume = Phase.Idle), listOf(Effect.Handle(e.text)))

        // 5. Not for the assistant. It is dropped: no log, no storage.
        return Step(s, listOf(Effect.Ignore))
    }
}
