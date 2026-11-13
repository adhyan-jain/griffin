package com.griffin.assistant.core

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.util.Random

class ListenerMachineTest {
    private fun run(s: LState, vararg events: LEvent): LStep {
        var cur = LStep(s)
        for (e in events) cur = ListenerMachine.reduce(cur.state, e)
        return cur
    }
    private val running = LState(active = true)

    @Test fun `start begins once and a second start does nothing`() {
        val a = run(LState(), LEvent.Start)
        assertTrue(a.state.active)
        assertEquals(listOf<LEffect>(LEffect.BeginNow), a.effects)
        assertEquals(emptyList<LEffect>(), ListenerMachine.reduce(a.state, LEvent.Start).effects)
    }

    @Test fun `stop with a restart pending lets the next start restart again (the old bug)`() {
        val pending = ListenerMachine.reduce(running, LEvent.Error(LError.Silence, false)).state
        assertTrue(pending.restartPending)
        val stopped = ListenerMachine.reduce(pending, LEvent.Stop).state
        assertFalse(stopped.restartPending)
        val restarted = run(stopped, LEvent.Start, LEvent.Results(listOf("hi")))
        assertTrue(restarted.effects.any { it is LEffect.Schedule })
    }

    @Test fun `only one restart is ever scheduled`() {
        val one = ListenerMachine.reduce(running, LEvent.Error(LError.Silence, false))
        val two = ListenerMachine.reduce(one.state, LEvent.Error(LError.Silence, false))
        assertEquals(1, one.effects.count { it is LEffect.Schedule })
        assertEquals(0, two.effects.count { it is LEffect.Schedule })
    }

    @Test fun `paused or stopped never schedules`() {
        assertEquals(0, ListenerMachine.reduce(running.copy(paused = true), LEvent.Error(LError.Silence, false)).effects.count { it is LEffect.Schedule })
        assertEquals(0, ListenerMachine.reduce(LState(), LEvent.Results(listOf("x"))).effects.count { it is LEffect.Schedule })
    }

    @Test fun `results are trimmed and blank ones dropped`() {
        val step = ListenerMachine.reduce(running, LEvent.Results(listOf("  hello ", "  ")))
        assertEquals(LEffect.Heard(listOf("hello")), step.effects.first())
        assertEquals(listOf(LEffect.Schedule(250, false)), step.effects.drop(1))
    }

    @Test fun `media playing waits unless forced`() {
        val wait = ListenerMachine.reduce(running, LEvent.BeginAttempt(mediaActive = true))
        assertTrue(wait.effects.any { it == LEffect.Schedule(2000, false) })
        val forced = ListenerMachine.reduce(running.copy(forceNext = true), LEvent.BeginAttempt(mediaActive = true))
        assertEquals(listOf<LEffect>(LEffect.StartRecognizer), forced.effects)
        assertFalse(forced.state.forceNext)
    }

    @Test fun `client errors fall back from on-device, then offline, then back off up to ten seconds`() {
        var s = running.copy(preferOnDevice = true, preferOffline = true)
        s = ListenerMachine.reduce(s, LEvent.Error(LError.Client, onDeviceUsable = true)).also { assertFalse(it.state.preferOnDevice) }.state.copy(restartPending = false)
        s = ListenerMachine.reduce(s, LEvent.Error(LError.Client, onDeviceUsable = true)).also { assertFalse(it.state.preferOffline) }.state.copy(restartPending = false)
        repeat(8) { s = ListenerMachine.reduce(s, LEvent.Error(LError.Client, false)).state.copy(restartPending = false) }
        val last = ListenerMachine.reduce(s, LEvent.Error(LError.Client, false))
        assertTrue(last.effects.contains(LEffect.Schedule(10_000, true)))
    }

    @Test fun `a missing language on the on-device engine falls back to the standard one`() {
        val first = ListenerMachine.reduce(running.copy(preferOnDevice = true), LEvent.Error(LError.Language, onDeviceUsable = true))
        assertFalse(first.state.preferOnDevice)
        assertTrue(first.effects.contains(LEffect.Schedule(500, true)))
        val second = ListenerMachine.reduce(first.state.copy(restartPending = false), LEvent.Error(LError.Language, onDeviceUsable = true))
        assertTrue(second.effects.contains(LEffect.Schedule(5000, true)))
    }

    @Test fun `a missing microphone permission ends listening`() {
        val step = ListenerMachine.reduce(running, LEvent.Error(LError.Permission, false))
        assertFalse(step.state.active)
        assertEquals(0, step.effects.count { it is LEffect.Schedule })
    }

    @Test fun `resume restarts only when paused and active`() {
        assertEquals(emptyList<LEffect>(), ListenerMachine.reduce(running, LEvent.Resume).effects)
        val r = ListenerMachine.reduce(running.copy(paused = true), LEvent.Resume)
        assertFalse(r.state.paused)
        assertEquals(listOf<LEffect>(LEffect.Schedule(350, false)), r.effects)
    }

    @Test fun `force listen cancels everything and begins now`() {
        val step = ListenerMachine.reduce(LState(paused = true, restartPending = true), LEvent.ForceListen)
        assertTrue(step.state.active); assertFalse(step.state.paused); assertFalse(step.state.restartPending); assertTrue(step.state.forceNext)
        assertEquals(listOf(LEffect.CancelTimers, LEffect.CancelListen, LEffect.BeginNow), step.effects)
    }

    @Test fun `fuzz keeps the invariants`() {
        val rnd = Random(7)
        val errors = LError.values()
        repeat(3000) { seed ->
            rnd.setSeed(seed.toLong())
            var s = LState()
            repeat(40) {
                val e: LEvent = when (rnd.nextInt(14)) {
                    0 -> LEvent.Start; 1 -> LEvent.Stop; 2 -> LEvent.Pause; 3 -> LEvent.Resume; 4 -> LEvent.ForceListen
                    5 -> LEvent.TimerFired(rnd.nextBoolean()); 6 -> LEvent.BeginAttempt(rnd.nextBoolean()); 7 -> LEvent.NoRecognizer
                    8 -> LEvent.StartFailed("x"); 9 -> LEvent.Ready; 10 -> LEvent.SpeechBegan
                    11 -> LEvent.Results(listOf("a")); else -> LEvent.Error(errors[rnd.nextInt(errors.size)], rnd.nextBoolean())
                }
                val step = ListenerMachine.reduce(s, e)
                assertTrue("at most one schedule per step", step.effects.count { it is LEffect.Schedule } <= 1)
                if (step.effects.any { it is LEffect.Schedule }) assertTrue("schedule sets pending", step.state.restartPending && step.state.active && !step.state.paused)
                if (!step.state.active) assertFalse("stopped means no pending restart", step.state.restartPending && e == LEvent.Stop)
                assertTrue(step.state.clientErrors >= 0)
                s = step.state
            }
        }
    }
}
