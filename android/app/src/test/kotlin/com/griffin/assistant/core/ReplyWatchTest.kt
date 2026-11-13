package com.griffin.assistant.core

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class ReplyWatchTest {
    @Test fun `only awaited whole names match`() {
        val w = ReplyWatch()
        w.update(listOf("Ada Lovelace", "  Sam  "))
        assertTrue(w.matches("ada  lovelace"))
        assertTrue(w.matches("SAM"))
        assertFalse(w.matches("Samantha"))
        assertFalse(w.matches("Mom"))
        assertFalse(w.matches(""))
    }

    @Test fun `nothing matches until the server has been asked`() {
        assertFalse(ReplyWatch().matches("Ada"))
    }
}
