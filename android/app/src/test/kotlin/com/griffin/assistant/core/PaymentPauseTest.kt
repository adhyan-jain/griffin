package com.griffin.assistant.core

import org.junit.Assert.assertEquals
import org.junit.Test

class PaymentPauseTest {
    private val canara = "com.canarabank.mobility"
    private val fromHome = PaymentPause.State(canara, since = 1_000, fromHome = true)
    private val fromUpiLink = PaymentPause.State(canara, since = 1_000, fromHome = false)
    private val done = fromHome.copy(relaunched = true)
    private val Wait = PaymentPause.Step.Wait
    private val Restore = PaymentPause.Step.Restore

    private fun step(s: PaymentPause.State, front: String?, now: Long, payment: Boolean = false, home: Boolean = false, transient: Boolean = false) =
        PaymentPause.next(s, front, now, payment, home, transient)

    @Test
    fun `a bank app opened from the home screen is started again fresh once, with Griffin off`() {
        val d = step(fromHome, canara, 1_500, payment = true)
        assertEquals(PaymentPause.Step.Relaunch(canara), d.step)
        assertEquals(Wait, step(d.state, canara, 2_000, payment = true).step)
    }

    @Test
    fun `a payment handed over by another app is never restarted while in front`() {
        assertEquals(Wait, step(fromUpiLink, canara, 1_500, payment = true).step)
    }

    @Test
    fun `a bank app that closed itself right away is opened again once`() {
        val d = step(fromUpiLink, "launcher", 2_000, home = true)
        assertEquals(PaymentPause.Step.Relaunch(canara), d.step)
        assertEquals(Wait, step(d.state, "launcher", 3_000, home = true).step)
    }

    @Test
    fun `stays off while the bank app or a system dialog is in front`() {
        assertEquals(Wait, step(done, canara, 60_000, payment = true).step)
        assertEquals(Wait, step(done, "com.android.systemui", 60_000, transient = true).step)
        assertEquals(Wait, step(done, null, 60_000).step)
    }

    @Test
    fun `the home screen turns Griffin back on at once`() {
        assertEquals(Restore, step(done, "launcher", 60_000, home = true).step)
    }

    @Test
    fun `a screen the bank app opens (a location prompt) does not turn Griffin back on`() {
        val gms = "com.google.android.gms"
        val d1 = step(done, gms, 9_000)
        assertEquals(Wait, d1.step)
        val d2 = step(d1.state, canara, 12_000, payment = true) // back in the bank app: the clock resets
        assertEquals(Wait, d2.step)
        assertEquals(Wait, step(d2.state, gms, 13_000).step)
    }

    @Test
    fun `another app that stays in front for a while turns Griffin back on`() {
        val wa = "com.whatsapp"
        val d1 = step(done, wa, 10_000)
        assertEquals(Wait, d1.step)
        assertEquals(Wait, step(d1.state, wa, 10_000 + PaymentPause.AWAY_MS - 1).step)
        assertEquals(Restore, step(d1.state, wa, 10_000 + PaymentPause.AWAY_MS).step)
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
