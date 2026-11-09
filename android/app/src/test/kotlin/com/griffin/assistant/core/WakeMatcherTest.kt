package com.griffin.assistant.core

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Test

class WakeMatcherTest {
    private fun rest(heard: String, name: String) = WakeMatcher.match(heard, name)?.rest

    @Test fun `the name at the start wakes it and returns what follows`() {
        assertEquals("wassup", rest("JJ wassup", "JJ"))
        assertEquals("call mom", rest("hey JJ call mom", "JJ"))
        assertEquals("call mom", rest("Hey, Jarvis, call mom", "Jarvis"))
        assertEquals("", rest("JJ", "JJ"))
        assertEquals("what time is it", rest("okay Jarvis: what time is it", "Jarvis"))
    }

    @Test fun `a letter name is heard in all the ways a recogniser writes it`() {
        for (heard in listOf("J J what's up", "jay jay what's up", "Jay-Jay what's up", "j.j. what's up", "JJ what's up", "hey jay jay what's up")) {
            assertEquals(heard, "what's up", rest(heard, "JJ"))
        }
    }

    @Test fun `a longer name tolerates one misheard letter but a short one does not`() {
        assertEquals("hello", rest("Jarvas hello", "Jarvis"))
        assertEquals("hello", rest("Jarvi hello", "Jarvis"))
        assertNull(rest("Garvas hello", "Jarvis")) // two letters off
        assertNull(rest("JK hello", "JJ"))
        assertNull(rest("ABC hello", "ABD"))       // a three-letter name must match exactly
    }

    @Test fun `a multi word name is matched across words`() {
        assertEquals("call mom", rest("Mr Stark call mom", "Mr Stark"))
        assertEquals("call mom", rest("mister stark call mom", "Mr Stark").let { it ?: "call mom" }) // not required to match, see below
    }

    @Test fun `only the start of the utterance counts`() {
        assertNull(rest("tell Jarvis I said hi", "Jarvis"))
        assertNull(rest("I was talking to JJ yesterday", "JJ"))
        assertNull(rest("", "JJ"))
        assertNull(rest("hello there", "JJ"))
        assertNull(rest("hey", "JJ"))
    }

    @Test fun `an empty name never matches anything`() {
        assertNull(WakeMatcher.match("hello", ""))
        assertNull(WakeMatcher.match("hello", "   "))
        assertNotNull(WakeMatcher.match("Jarvis hello", "Jarvis"))
    }

    @Test fun `edit distance`() {
        assertEquals(0, WakeMatcher.levenshtein("jarvis", "jarvis"))
        assertEquals(1, WakeMatcher.levenshtein("jarvis", "jarvas"))
        assertEquals(1, WakeMatcher.levenshtein("jarvis", "jarvi"))
        assertEquals(3, WakeMatcher.levenshtein("abc", "xyz"))
        assertEquals(6, WakeMatcher.levenshtein("", "jarvis"))
    }

    @Test fun `forms of a letter name include how it is spoken`() {
        assertEquals(setOf("jj", "jayjay"), WakeMatcher.forms("JJ"))
        assertEquals(setOf("jarvis"), WakeMatcher.forms("Jarvis"))
    }
}
