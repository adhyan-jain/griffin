package com.griffin.assistant.core

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class PhoneJobTest {
    private class FakeScreen : PScreenDriver {
        val did = mutableListOf<PStep>()
        override fun snapshot() = PScreen("com.notes", "Notes", "Milk", listOf(PElement("3", "textbox", "Note"), PElement("7", "button", "Save")))
        override fun perform(step: PStep): String? { did += step; return null }
    }
    private class Scripted(val moves: List<PDecision>) : PBrain {
        var i = 0
        override fun next(goal: String, screen: PScreen, done: List<PStep>) = moves[i++]
    }

    @Test fun `digest matches the server's known answer`() {
        val steps = listOf(
            PStep("open", text = "Notes"),
            PStep("type", ref = "3", role = "textbox", name = "Note", text = "buy milk — 2 L"),
            PStep("click", ref = "7", role = "button", name = "Save", final = true),
        )
        // computed by domain.ScriptDigest in Go: the approval is bound to this value on both sides
        assertEquals("8521c6f40ff6d9d3002095ecfc14cdd7", PhoneScript.digest("add milk to Notes", steps))
    }

    @Test fun `stops before the final step and never performs it`() {
        val screen = FakeScreen()
        val brain = Scripted(listOf(
            PDecision(false, "", PStep("type", ref = "3", text = "buy milk")),
            PDecision(false, "", PStep("click", ref = "7", final = true)),
        ))
        val run = PhoneScript.run("add milk", screen, brain) as PRun.Staged
        assertTrue(screen.did.none { it.final })
        assertEquals(2, run.steps.size)
        assertEquals("textbox", run.steps[0].role)
        assertEquals("Save", run.steps[1].name)
        assertEquals(PhoneScript.digest("add milk", run.steps), run.digest)
    }

    @Test fun `the code forces final on a send-like tap whatever the model says`() {
        val screen = FakeScreen()
        val brain = Scripted(listOf(PDecision(false, "", PStep("click", ref = "7", final = false))))
        // "Save" is not send-like, so use a screen whose button says Send
        val sendScreen = object : PScreenDriver {
            override fun snapshot() = PScreen("chat", "Chat", "", listOf(PElement("1", "button", "Send")))
            override fun perform(step: PStep): String? { screen.did += step; return null }
        }
        val run = PhoneScript.run("g", sendScreen, Scripted(listOf(PDecision(false, "", PStep("click", ref = "1")))))
        assertTrue(run is PRun.Staged && screen.did.isEmpty())
        assertFalse(PhoneScript.irreversible(PStep("click", name = "Next")))
        assertTrue(PhoneScript.irreversible(PStep("press", text = "Enter")))
        assertNotNull(brain)
    }

    @Test fun `an unknown element, a budget overrun and an unreadable screen fail`() {
        assertTrue(PhoneScript.run("g", FakeScreen(), Scripted(listOf(PDecision(false, "", PStep("click", ref = "99"))))) is PRun.Failed)
        val waits = List(3) { PDecision(false, "", PStep("wait")) }
        assertTrue(PhoneScript.run("g", FakeScreen(), Scripted(waits), maxSteps = 3) is PRun.Failed)
        val blind = object : PScreenDriver { override fun snapshot() = null; override fun perform(step: PStep) = null }
        assertTrue(PhoneScript.run("g", blind, Scripted(emptyList())) is PRun.Failed)
    }

    @Test fun `done returns the answer`() {
        assertEquals(PRun.Answer("3 notes"), PhoneScript.run("count", FakeScreen(), Scripted(listOf(PDecision(true, "3 notes", PStep("wait"))))))
    }

    @Test fun `replay performs exactly the script and refuses a malformed one`() {
        val screen = FakeScreen()
        val steps = listOf(PStep("open", text = "Notes"), PStep("click", ref = "7", name = "Save", final = true))
        assertNull(PhoneScript.replay(screen, steps))
        assertEquals(steps, screen.did)
        assertNotNull(PhoneScript.replay(FakeScreen(), listOf(PStep("click", final = true), PStep("wait"))))
        assertNotNull(PhoneScript.replay(FakeScreen(), emptyList()))
    }

    @Test fun `json round trip keeps every field that the digest covers`() {
        val steps = listOf(PStep("type", "3", "textbox", "Note", "héllo", "", "why", false), PStep("click", "7", "button", "Save", final = true))
        val back = PhoneScript.fromJson(PhoneScript.toJson(steps))
        assertEquals(PhoneScript.digest("g", steps), PhoneScript.digest("g", back))
    }
}
