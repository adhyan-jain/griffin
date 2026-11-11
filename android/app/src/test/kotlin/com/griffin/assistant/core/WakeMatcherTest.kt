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
        assertEquals("send hello to bhai", rest("bhosdike send hello to bhai", "JJ"))
        assertEquals("", rest("oye bhosadike", "JJ"))
        assertEquals("", rest("bhosdike", "JJ"))
        assertEquals("what's up", rest("hey griffin what's up", "JJ"))
    }

    @Test fun `profanity censorship asterisks are unmasked and match`() {
        assertEquals("change spotify song", rest("oye b*** change spotify song", "JJ"))
        assertEquals("change spotify song", rest("oye b**** change spotify song", "JJ"))
        assertEquals("change spotify song", rest("oye b***dike change spotify song", "JJ"))
        assertEquals("change spotify song", rest("b*** change spotify song", "JJ"))
        assertEquals("change spotify song", rest("b**** change spotify song", "JJ"))
        assertEquals("change spotify song", rest("oye *** change spotify song", "JJ"))
    }

    @Test fun `english acoustic misrecognitions by speech engine match`() {
        assertEquals("play music", rest("boss dk play music", "JJ"))
        assertEquals("play music", rest("boss decay play music", "JJ"))
        assertEquals("play music", rest("boss dyke play music", "JJ"))
        assertEquals("play music", rest("bose dk play music", "JJ"))
        assertEquals("play music", rest("oye boss dk play music", "JJ"))
        assertEquals("play music", rest("oye boss decay play music", "JJ"))
        assertEquals("play music", rest("oi boss dk play music", "JJ"))
        assertEquals("play music", rest("oh yeah boss decay play music", "JJ"))
        assertEquals("play music", rest("oye b s d k play music", "JJ"))
    }

    @Test fun `devanagari wake phrases match`() {
        assertEquals("play music", rest("ओए भोसड़ीके play music", "JJ"))
        assertEquals("play music", rest("भोसड़ीके play music", "JJ"))
        assertEquals("play music", rest("भोसडीके play music", "JJ"))
    }

    @Test fun `matchAny finds wake among multiple speech hypotheses`() {
        val hypotheses = listOf("oil boss dick", "oye boss dk skip track", "boy's deck")
        val match = WakeMatcher.matchAny(hypotheses, "JJ")
        assertEquals("skip track", match?.rest)
    }
}
