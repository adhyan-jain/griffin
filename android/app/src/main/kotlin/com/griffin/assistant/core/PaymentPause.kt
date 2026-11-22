package com.griffin.assistant.core

/**
 * Bank and UPI apps refuse to run while any accessibility service is on: they show "accessibility permission
 * detected" or close themselves. Griffin steps aside. When one opens, Griffin's accessibility is switched off.
 * The bank app may already have seen Griffin and be showing that warning, so when it was opened from the home
 * screen (or it closed itself) it is opened again once, fresh, now with Griffin off. When the owner leaves it,
 * Griffin's accessibility comes back. This file is the decision logic only, so it can be tested without a phone.
 */
object PaymentPause {
    /** How long after Griffin steps aside a bank app that vanished is treated as "it closed itself because of us". */
    const val RELAUNCH_WINDOW_MS = 6_000L
    /**
     * Bank apps open other screens of their own (a Google location prompt, the camera for a QR code, a contact
     * picker). Another app in front counts as "left" only after it has stayed there this long; the home screen
     * counts at once.
     */
    const val AWAY_MS = 20_000L

    sealed class Step {
        object Wait : Step()
        /** Open the bank app again, fresh: its start-up check saw Griffin. */
        data class Relaunch(val pkg: String) : Step()
        /** The owner left the bank app: switch Griffin's accessibility back on. */
        object Restore : Step()
    }

    /**
     * [fromHome] is true when the bank app was opened from the home screen. It is false when another app handed
     * it a payment (a UPI link), which a fresh start would lose, so that is never restarted while in front.
     * [awaySince] is when some other app came to the front, while it stays there.
     */
    data class State(val pkg: String, val since: Long, val fromHome: Boolean, val relaunched: Boolean = false, val awaySince: Long? = null)

    data class Decision(val step: Step, val state: State)

    /**
     * What to do now, given the app in front. [isPayment], [isHome] and [isTransient] classify [front]; null
     * [front] means it is not known yet.
     */
    fun next(state: State, front: String?, now: Long, isPayment: Boolean, isHome: Boolean, isTransient: Boolean): Decision {
        val early = now - state.since < RELAUNCH_WINDOW_MS
        if (!state.relaunched && early) {
            if (front == state.pkg && state.fromHome) return Decision(Step.Relaunch(state.pkg), state.copy(relaunched = true))
            if (isHome) return Decision(Step.Relaunch(state.pkg), state.copy(relaunched = true)) // it closed itself
        }
        if (front == null || isTransient) return Decision(Step.Wait, state)
        if (isPayment) return Decision(Step.Wait, state.copy(awaySince = null))
        if (isHome) return if (early) Decision(Step.Wait, state) else Decision(Step.Restore, state)
        val away = state.awaySince ?: now
        if (now - away >= AWAY_MS) return Decision(Step.Restore, state)
        return Decision(Step.Wait, state.copy(awaySince = away))
    }

    /** The enabled-services setting with [component] removed (a colon-separated list). */
    fun without(setting: String?, component: String): String =
        (setting ?: "").split(':').filter { it.isNotBlank() && !it.equals(component, ignoreCase = true) }.joinToString(":")

    /** The enabled-services setting with [component] present exactly once, keeping everyone else's services. */
    fun with(setting: String?, component: String): String {
        val rest = without(setting, component)
        return if (rest.isEmpty()) component else "$rest:$component"
    }
}
