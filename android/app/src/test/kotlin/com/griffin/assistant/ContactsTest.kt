package com.griffin.assistant

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class ContactsTest {

    @Test
    fun testExactFullName() {
        val matches = listOf(
            Contacts.Match("Alice Smith", "+15550101"),
            Contacts.Match("Bob Jones", "+15550102")
        )
        val res = Contacts.resolveMatches("Alice Smith", matches)
        assertTrue(res is Contacts.Resolution.Resolved)
        assertEquals("+15550101", (res as Contacts.Resolution.Resolved).match.number)
    }

    @Test
    fun testUniquePartialName() {
        val matches = listOf(
            Contacts.Match("Alice Smith", "+15550101")
        )
        val res = Contacts.resolveMatches("Alice", matches)
        assertTrue(res is Contacts.Resolution.Resolved)
        assertEquals("Alice Smith", (res as Contacts.Resolution.Resolved).match.name)
    }

    @Test
    fun testTwoPeopleSharingFirstName() {
        val matches = listOf(
            Contacts.Match("Alice Smith", "+15550101"),
            Contacts.Match("Alice Johnson", "+15550102")
        )
        val res = Contacts.resolveMatches("Alice", matches)
        assertTrue("Sharing first name without exact full name match must be Ambiguous", res is Contacts.Resolution.Ambiguous)
        val amb = res as Contacts.Resolution.Ambiguous
        assertEquals(2, amb.candidates.size)
    }

    @Test
    fun testDuplicateFullNamesDifferentNumbers() {
        // Two distinct people or entries with the exact same name but different phone numbers
        val matches = listOf(
            Contacts.Match("John Doe", "+15551111"),
            Contacts.Match("John Doe", "+15552222")
        )
        val res = Contacts.resolveMatches("John Doe", matches)
        assertTrue("Duplicate full name with different numbers must be Ambiguous", res is Contacts.Resolution.Ambiguous)
    }

    @Test
    fun testDuplicatePhoneRowsSameContact() {
        // Same person duplicated across SIM and Google accounts with identical number
        val matches = listOf(
            Contacts.Match("Rohan Sharma", "+919876543210"),
            Contacts.Match("Rohan Sharma", "09876543210")
        )
        val res = Contacts.resolveMatches("Rohan Sharma", matches)
        assertTrue("Duplicate cards with same number should resolve cleanly", res is Contacts.Resolution.Resolved)
        assertEquals("Rohan Sharma", (res as Contacts.Resolution.Resolved).match.name)
    }

    @Test
    fun testMultiPartIndianNames() {
        val matches = listOf(
            Contacts.Match("Aarav Kumar Patel", "+919811122233"),
            Contacts.Match("Aarav Sharma", "+919844455566")
        )
        val res = Contacts.resolveMatches("Aarav Kumar Patel", matches)
        assertTrue(res is Contacts.Resolution.Resolved)
        assertEquals("+919811122233", (res as Contacts.Resolution.Resolved).match.number)
    }

    @Test
    fun testNoMatch() {
        val matches = emptyList<Contacts.Match>()
        val res = Contacts.resolveMatches("NonExistent", matches)
        assertTrue(res is Contacts.Resolution.NotFound)
    }

    @Test
    fun testEmptyQuery() {
        val matches = listOf(Contacts.Match("Alice", "+123456"))
        val res = Contacts.resolveMatches("   ", matches)
        assertTrue(res is Contacts.Resolution.NotFound)
    }

    @Test
    fun testWhatsAppMatchingWithDataId() {
        val matches = listOf(
            Contacts.Match("Siddharth Rao", "+919988776655", whatsAppDataId = 42L)
        )
        val res = Contacts.resolveMatches("Siddharth Rao", matches)
        assertTrue(res is Contacts.Resolution.Resolved)
        assertEquals(42L, (res as Contacts.Resolution.Resolved).match.whatsAppDataId)
    }

    @Test
    fun testWhatsAppMatchingAmbiguityRefusal() {
        val matches = listOf(
            Contacts.Match("Rahul Verma", "+919911111111", whatsAppDataId = 101L),
            Contacts.Match("Rahul Sharma", "+919922222222", whatsAppDataId = 102L)
        )
        val res = Contacts.resolveMatches("Rahul", matches)
        assertTrue("WhatsApp matching with multiple plausible matches must refuse ambiguity", res is Contacts.Resolution.Ambiguous)
    }
}
