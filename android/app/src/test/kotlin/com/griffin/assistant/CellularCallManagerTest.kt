package com.griffin.assistant

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class CellularCallManagerTest {

    @Test
    fun `detects questions and unknown context requiring fallback`() {
        val questions = listOf(
            "why are you calling?",
            "Where is he right now?",
            "what time will he reach?",
            "who is this?",
            "how do I get there?",
            "which meeting are you talking about?",
            "whose phone is this?",
            "tell me more about it",
            "what time is the flight?",
            "Is everything alright?",
            "kya hua?",
            "kyun phone kiya tha",
            "kab aa rahe ho",
            "kaha pe milna hai",
            "kaise aana hai",
        )
        for (q in questions) {
            assertTrue("Expected question detection for: $q", CellularCallManager.isQuestionOrUnknownContext(q))
        }
    }

    @Test
    fun `ordinary non-question statements do not trigger fallback`() {
        val statements = listOf(
            "okay",
            "sure, got it",
            "I will be there in ten minutes",
            "tell him thank you",
            "noted, thanks",
            "I'm on my way",
            "alright, bye",
            "",
            "   ",
        )
        for (s in statements) {
            assertFalse("Expected non-question for: $s", CellularCallManager.isQuestionOrUnknownContext(s))
        }
    }

    @Test
    fun `greeting format matches specification`() {
        val greeting = CellularCallManager.buildGreeting("Adhyan", "The deployment finished successfully")
        assertEquals(
            "Hello, this is Griffin calling on behalf of Adhyan. The deployment finished successfully. Is there anything you would like me to convey back to Adhyan?",
            greeting,
        )
    }

    @Test
    fun `fallback format matches specification`() {
        val fallback = CellularCallManager.buildFallback("Adhyan")
        assertEquals(
            "I will ask Adhyan and reply in a while, is there anything else you want to mention?",
            fallback,
        )
    }

    @Test
    fun `closing format matches specification`() {
        val closing = CellularCallManager.buildClosing("Adhyan")
        assertEquals(
            "Thank you. I have noted that and will inform Adhyan. Goodbye.",
            closing,
        )
    }

    @Test
    fun `owner display name resolves correctly`() {
        assertEquals("Adhyan", CellularCallManager.ownerDisplayName("Adhyan", "user@example.com"))
        assertEquals("Adhyan Jain", CellularCallManager.ownerDisplayName(null, "adhyan.jain@example.com"))
        assertEquals("John Doe", CellularCallManager.ownerDisplayName("", "john_doe@example.com"))
        assertEquals("the owner", CellularCallManager.ownerDisplayName(null, ""))
        assertEquals("the owner", CellularCallManager.ownerDisplayName(null, "invalid-email"))
    }
}
