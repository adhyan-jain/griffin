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
     */
    data class State(val pkg: String, val since: Long, val fromHome: Boolean, val relaunched: Boolean = false)

    /**
     * What to do now, given the app in front. [isPayment], [isHome] and [isTransient] classify [front]; null
     * [front] means it is not known yet.
     */
    fun next(state: State, front: String?, now: Long, isPayment: Boolean, isHome: Boolean, isTransient: Boolean): Step {
        val early = now - state.since < RELAUNCH_WINDOW_MS
        if (!state.relaunched && early) {
            if (front == state.pkg && state.fromHome) return Step.Relaunch(state.pkg)
            if (isHome) return Step.Relaunch(state.pkg) // it closed itself
        }
        if (front == null || isTransient || isPayment) return Step.Wait
        if (isHome && early) return Step.Wait // give the reopened app a moment to come up
        return Step.Restore
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
