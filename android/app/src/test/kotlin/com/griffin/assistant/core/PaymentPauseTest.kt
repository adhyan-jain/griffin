package com.griffin.assistant.core

import org.junit.Assert.assertEquals
import org.junit.Test

class PaymentPauseTest {
    private val canara = "com.canarabank.mobility"
    private val fromHome = PaymentPause.State(canara, since = 1_000, fromHome = true)
    private val fromUpiLink = PaymentPause.State(canara, since = 1_000, fromHome = false)
    private val Wait = PaymentPause.Step.Wait
    private val Restore = PaymentPause.Step.Restore

    @Test
    fun `a bank app opened from the home screen is started again fresh once, with Griffin off`() {
        assertEquals(PaymentPause.Step.Relaunch(canara), PaymentPause.next(fromHome, canara, 1_500, isPayment = true, isHome = false, isTransient = false))
        val again = fromHome.copy(relaunched = true)
        assertEquals(Wait, PaymentPause.next(again, canara, 2_000, isPayment = true, isHome = false, isTransient = false))
    }

    @Test
    fun `a payment handed over by another app is never restarted while in front`() {
        assertEquals(Wait, PaymentPause.next(fromUpiLink, canara, 1_500, isPayment = true, isHome = false, isTransient = false))
    }

    @Test
    fun `a bank app that closed itself right away is opened again once`() {
        assertEquals(PaymentPause.Step.Relaunch(canara), PaymentPause.next(fromUpiLink, "launcher", 2_000, isPayment = false, isHome = true, isTransient = false))
        assertEquals(Wait, PaymentPause.next(fromUpiLink.copy(relaunched = true), "launcher", 3_000, isPayment = false, isHome = true, isTransient = false))
    }

    @Test
    fun `stays off while the bank app or a system dialog is in front`() {
        val done = fromHome.copy(relaunched = true)
        assertEquals(Wait, PaymentPause.next(done, canara, 60_000, isPayment = true, isHome = false, isTransient = false))
        assertEquals(Wait, PaymentPause.next(done, "com.android.systemui", 60_000, isPayment = false, isHome = false, isTransient = true))
        assertEquals(Wait, PaymentPause.next(done, null, 60_000, isPayment = false, isHome = false, isTransient = false))
    }

    @Test
    fun `leaving the bank app turns Griffin back on`() {
        val done = fromHome.copy(relaunched = true)
        assertEquals(Restore, PaymentPause.next(done, "launcher", 60_000, isPayment = false, isHome = true, isTransient = false))
        assertEquals(Restore, PaymentPause.next(fromUpiLink, "com.whatsapp", 2_000, isPayment = false, isHome = false, isTransient = false))
    }

    @Test
    fun `the accessibility setting keeps other services and lists Griffin once`() {
        val g = "com.griffin.assistant/com.griffin.assistant.GriffinAccess"
        val other = "com.other/.Svc"
        assertEquals(other, PaymentPause.without("$other:$g", g))
        assertEquals("", PaymentPause.without(g, g))
        assertEquals("$other:$g", PaymentPause.with(other, g))
        assertEquals("$other:$g", PaymentPause.with("$g:$other", g))
        assertEquals(g, PaymentPause.with(null, g))
    }
}
