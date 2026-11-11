package com.griffin.assistant.core

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class WakeMatcherTest {
    private fun rest(heard: String, name: String) = WakeMatcher.match(heard, name)?.rest

    @Test fun `hey and the name wake it and return what follows`() {
        assertEquals("wassup", rest("hey JJ wassup", "JJ"))
        assertEquals("call mom", rest("Hey, Jarvis, call mom", "Jarvis"))
        assertEquals("", rest("hey JJ", "JJ"))
        assertEquals("what time is it", rest("hey Jarvis: what time is it", "Jarvis"))
    }

    @Test fun `the name alone never wakes it`() {
        assertNull(rest("JJ wassup", "JJ"))
        assertNull(rest("Jarvis call mom", "Jarvis"))
        assertNull(rest("okay Jarvis call mom", "Jarvis"))
        assertNull(rest("JJ", "JJ"))
    }

    @Test fun `a letter name is heard in all the ways a recogniser writes it`() {
        for (heard in listOf("hey J J what's up", "hey jay jay what's up", "hey Jay-Jay what's up", "hey j.j. what's up", "hay jj what's up")) {
            assertEquals(heard, "what's up", rest(heard, "JJ"))
        }
    }

    @Test fun `a longer name tolerates one misheard letter but a short one does not`() {
        assertEquals("hello", rest("hey Jarvas hello", "Jarvis"))
        assertEquals("hello", rest("hey Jarvi hello", "Jarvis"))
        assertNull(rest("hey Garvas hello", "Jarvis"))
        assertNull(rest("hey JK hello", "JJ"))
        assertNull(rest("hey ABC hello", "ABD"))
    }

    @Test fun `a multi word name is matched across words`() {
        assertEquals("call mom", rest("hey Mr Stark call mom", "Mr Stark"))
    }

    @Test fun `only the start of the utterance counts`() {
        assertNull(rest("tell Jarvis I said hi", "Jarvis"))
        assertNull(rest("I said hey Jarvis yesterday", "Jarvis"))
        assertNull(rest("hey there", "JJ"))
        assertNull(rest("", "JJ"))
        assertNull(rest("hey", "JJ"))
    }

    @Test fun `an empty name never matches anything`() {
        assertNull(WakeMatcher.match("hey hello", ""))
        assertNull(WakeMatcher.match("hey hello", "   "))
    }

    @Test fun `edit distance`() {
        assertEquals(0, WakeMatcher.levenshtein("jarvis", "jarvis"))
        assertEquals(1, WakeMatcher.levenshtein("jarvis", "jarvas"))
        assertEquals(3, WakeMatcher.levenshtein("abc", "xyz"))
        assertEquals(6, WakeMatcher.levenshtein("", "jarvis"))
    }

    @Test fun `forms of a letter name include how it is spoken and griffin`() {
        assertEquals(setOf("jj", "jayjay", "griffin"), WakeMatcher.forms("JJ"))
        assertEquals(setOf("jarvis", "griffin"), WakeMatcher.forms("Jarvis"))
    }

    @Test fun `oye bhosadike wakes and extracts command`() {
        assertEquals("send hello to bhai", rest("oye bhosadike send hello to bhai", "JJ"))
        assertEquals("send hello to bhai", rest("oye bhosdike send hello to bhai", "JJ"))
        assertEquals("send hello to bhai", rest("oye bsdk send hello to bhai", "JJ"))
        assertEquals("send hello to bhai", rest("bhosadike send hello to bhai", "JJ"))
        assertEquals("", rest("oye bhosadike", "JJ"))
        assertEquals("what's up", rest("hey griffin what's up", "JJ"))
    }
}
