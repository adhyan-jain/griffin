package com.griffin.assistant.core

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.util.Random

class AssistantMachineTest {
    private val name = "JJ"
    private val sms = PendingAction(PendingAction.Kind.Sms, "Mom", "+911234567890", "running late", "sms")
    private fun heard(s: AState, text: String, now: Long = 1000) = AssistantMachine.reduce(s, AEvent.Heard(text, now, name))

    @Test fun `ordinary talk is ignored and changes nothing`() {
        val s = AState()
        for (t in listOf("what time is the meeting", "JJ what's up", "tell JJ hello", "okay JJ call mom")) {
            val step = heard(s, t)
            assertEquals(t, s, step.state)
            assertEquals(t, listOf(Effect.Ignore), step.effects)
        }
    }

    @Test fun `hey and the name with a request goes straight to thinking`() {
        val step = heard(AState(), "hey JJ what's the capital of France")
        assertEquals(Phase.Thinking, step.state.phase)
        assertEquals(listOf(Effect.Beep, Effect.Handle("what's the capital of France")), step.effects)
    }

    @Test fun `hey and the name alone listens for eight seconds`() {
        val step = heard(AState(), "hey JJ", now = 5000)
        assertEquals(AState(phase = Phase.Awake, until = 13_000), step.state)
        assertEquals(listOf<Effect>(Effect.Beep), step.effects)
        // the next sentence is the request, with no wake phrase
        val next = heard(step.state, "set a timer for five minutes", now = 9000)
        assertEquals(Phase.Thinking, next.state.phase)
        assertEquals(listOf<Effect>(Effect.Handle("set a timer for five minutes")), next.effects)
        // after the window, a bare sentence is ignored again
        val late = heard(step.state, "set a timer", now = 13_001)
        assertEquals(Phase.Idle, late.state.phase)
        assertEquals(listOf<Effect>(Effect.Ignore), late.effects)
    }

    @Test fun `a tick ends an expired listening window`() {
        val awake = heard(AState(), "hey JJ", now = 0).state
        assertEquals(Phase.Awake, AssistantMachine.reduce(awake, AEvent.Tick(7999)).state.phase)
        assertEquals(Phase.Idle, AssistantMachine.reduce(awake, AEvent.Tick(8000)).state.phase)
    }

    @Test fun `after a reply it listens on for a follow-up, then goes quiet`() {
        var s = heard(AState(), "hey JJ hello").state
        assertEquals(Phase.Thinking, s.phase)
        s = AssistantMachine.reduce(s, AEvent.SpeechStarted).state
        assertEquals(Phase.Speaking, s.phase)
        s = AssistantMachine.reduce(s, AEvent.SpeechEnded(5000)).state
        assertEquals(Phase.Awake, s.phase)
        assertEquals(5000 + AssistantMachine.FOLLOWUP_MS, s.until)
        // a follow-up needs no wake phrase
        val next = heard(s, "and what about Spain", now = 8000)
        assertEquals(listOf<Effect>(Effect.Handle("and what about Spain")), next.effects)
        // silence ends it
        assertEquals(Phase.Idle, AssistantMachine.reduce(s, AEvent.Tick(5000 + AssistantMachine.FOLLOWUP_MS)).state.phase)
        assertEquals(listOf<Effect>(Effect.Ignore), heard(s, "so anyway", now = 20_000).effects)
    }

    @Test fun `thanks or that's all ends the conversation`() {
        val awake = AssistantMachine.reduce(AState(phase = Phase.Awake, until = 99_000), AEvent.Tick(0)).state
        val step = heard(awake, "thanks", now = 1000)
        assertEquals(listOf<Effect>(Effect.Handle("thanks")), step.effects)
        assertEquals(Command.Stop, Commands.parse("thanks"))
        assertEquals(Command.Stop, Commands.parse("that's all"))
        assertEquals(Phase.Idle, AssistantMachine.reduce(step.state, AEvent.WorkFinished).state.phase)
    }

    @Test fun `a button wakes it without saying anything, and cancels a pending confirmation`() {
        val woke = AssistantMachine.reduce(AState(), AEvent.ManualWake(now = 2000))
        assertEquals(AState(phase = Phase.Awake, until = 2000 + AssistantMachine.AWAKE_MS), woke.state)
        assertEquals(listOf<Effect>(Effect.Beep), woke.effects)
        // the next sentence is the request
        assertEquals(listOf<Effect>(Effect.Handle("what's the weather")), heard(woke.state, "what's the weather", now = 3000).effects)
        // pressing it while a text waits for "send" cancels that text
        val confirming = AssistantMachine.reduce(AState(), AEvent.ConfirmRequested(sms, now = 0)).state
        val pressed = AssistantMachine.reduce(confirming, AEvent.ManualWake(now = 1000)).state
        assertNull(pressed.pending)
        assertEquals(Phase.Awake, pressed.phase)
    }

    @Test fun `work that ends with nothing to say returns to idle`() {
        val s = heard(AState(), "hey JJ open the camera").state
        assertEquals(AState(), AssistantMachine.reduce(s, AEvent.WorkFinished).state)
        assertEquals("only thinking is ended by it", AState(), AssistantMachine.reduce(AState(), AEvent.WorkFinished).state)
    }

    @Test fun `speech that starts while idle stays invisible`() {
        // a call announcement or an alarm must not show the listening animation
        val s = AssistantMachine.reduce(AState(), AEvent.SpeechStarted).state
        assertEquals(Phase.Idle, s.phase)
        assertEquals(Phase.Idle, AssistantMachine.reduce(s, AEvent.SpeechEnded(0)).state.phase)
    }

    @Test fun `a confirmation survives the question being spoken and is then answered`() {
        var s = AssistantMachine.reduce(AState(phase = Phase.Thinking), AEvent.ConfirmRequested(sms, now = 1000)).state
        assertEquals(Phase.Confirming, s.phase)
        s = AssistantMachine.reduce(s, AEvent.SpeechStarted).state
        assertEquals(Phase.Speaking, s.phase)
        s = AssistantMachine.reduce(s, AEvent.SpeechEnded(3000)).state
        assertEquals("back to waiting for the yes, so the animation stays", Phase.Confirming, s.phase)
        assertEquals(sms, s.pending)

        val yes = heard(s, "Send it.", now = 4000)
        assertEquals(listOf<Effect>(Effect.Confirmed(sms)), yes.effects)
        assertEquals(Phase.Thinking, yes.state.phase)
        assertNull(yes.state.pending)

        val no = heard(s, "no", now = 4000)
        assertEquals(listOf<Effect>(Effect.Declined(sms)), no.effects)
        assertNull(no.state.pending)
    }

    @Test fun `any other sentence cancels the wait and is treated as ordinary input`() {
        val s = AssistantMachine.reduce(AState(), AEvent.ConfirmRequested(sms, now = 0)).state
        val step = heard(s, "hey JJ what time is it", now = 1000)
        assertNull("the pending text is dropped, never sent later", step.state.pending)
        assertEquals(listOf(Effect.Beep, Effect.Handle("what time is it")), step.effects)
        val chatter = heard(s, "so anyway", now = 1000)
        assertEquals(Phase.Idle, chatter.state.phase)
        assertEquals(listOf<Effect>(Effect.Ignore), chatter.effects)
    }

    @Test fun `a new request while the confirmation question is being spoken cancels it`() {
        // found by the fuzz test below: the pending text used to linger in the state
        var s = AssistantMachine.reduce(AState(), AEvent.ConfirmRequested(sms, now = 0)).state
        s = AssistantMachine.reduce(s, AEvent.SpeechStarted).state
        assertEquals(Phase.Speaking, s.phase)
        val step = heard(s, "hey J J set a timer", now = 1000)
        assertEquals(Phase.Thinking, step.state.phase)
        assertNull(step.state.pending)
        assertEquals(Phase.Idle, step.state.resume)
    }

    @Test fun `a confirmation that timed out is not accepted`() {
        val s = AssistantMachine.reduce(AState(), AEvent.ConfirmRequested(sms, now = 0)).state
        val step = heard(s, "send it", now = AssistantMachine.CONFIRM_MS + 1)
        assertTrue("an old yes must never send anything", step.effects.none { it is Effect.Confirmed })
        assertEquals(Phase.Idle, step.state.phase)
        assertEquals(Phase.Idle, AssistantMachine.reduce(s, AEvent.Tick(AssistantMachine.CONFIRM_MS)).state.phase)
    }

    @Test fun `while a call rings answer and reject need no wake phrase, and only then`() {
        val ringing = AssistantMachine.reduce(AState(), AEvent.CallRinging(now = 1000)).state
        assertEquals(listOf<Effect>(Effect.AnswerCall), heard(ringing, "pick up", 2000).effects)
        assertEquals(listOf<Effect>(Effect.RejectCall), heard(ringing, "decline", 2000).effects)
        assertEquals(0L, heard(ringing, "answer", 2000).state.ringingUntil)
        // not ringing (or the window passed): "answer" is just talk
        assertEquals(listOf<Effect>(Effect.Ignore), heard(AState(), "answer", 2000).effects)
        assertEquals(listOf<Effect>(Effect.Ignore), heard(ringing, "answer", 1000 + AssistantMachine.RINGING_MS + 1).effects)
        // other talk during a ring still needs the wake phrase
        assertEquals(listOf<Effect>(Effect.Ignore), heard(ringing, "what a noise", 2000).effects)
    }

    @Test fun `the animation follows the phase`() {
        assertEquals("hidden", AssistantMachine.orb(Phase.Idle))
        assertEquals("listening", AssistantMachine.orb(Phase.Awake))
        assertEquals("listening", AssistantMachine.orb(Phase.Confirming))
        assertEquals("thinking", AssistantMachine.orb(Phase.Thinking))
        assertEquals("speaking", AssistantMachine.orb(Phase.Speaking))
    }

    /** Whatever happens, in any order, the state stays coherent. */
    @Test fun `random event sequences never break the invariants`() {
        val rnd = Random(42)
        val sentences = listOf("hey JJ", "hey JJ hello", "hey J J set a timer", "send it", "no", "pick up", "answer", "random chatter", "", "hey Jarvis hi")
        repeat(3000) {
            var s = AState()
            var now = 0L
            repeat(40) {
                now += rnd.nextInt(9000).toLong()
                val e: AEvent = when (rnd.nextInt(8)) {
                    0, 1, 2 -> AEvent.Heard(sentences[rnd.nextInt(sentences.size)], now, name)
                    3 -> AEvent.Tick(now)
                    4 -> AEvent.SpeechStarted
                    5 -> AEvent.SpeechEnded(now)
                    6 -> AEvent.ConfirmRequested(sms, now)
                    else -> when (rnd.nextInt(3)) { 0 -> AEvent.WorkFinished; 1 -> AEvent.CallRinging(now); else -> AEvent.ManualWake(now) }
                }
                s = AssistantMachine.reduce(s, e).state
                // The pending text exists while waiting for the yes, and while the question is being spoken (Speaking that
                // will return to Confirming); at no other time.
                val holdsPending = s.phase == Phase.Confirming || (s.phase == Phase.Speaking && s.resume == Phase.Confirming)
                assertEquals("pending is held exactly while confirming ($s after $e)", holdsPending, s.pending != null)
                if (s.phase == Phase.Idle) assertEquals("idle has no deadline ($s)", 0L, s.until)
                if (s.phase == Phase.Awake || s.phase == Phase.Confirming) assertTrue("a waiting phase has a deadline ($s)", s.until > 0)
            }
        }
    }
}
