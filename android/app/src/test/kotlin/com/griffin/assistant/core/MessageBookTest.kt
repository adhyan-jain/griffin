package com.griffin.assistant.core

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class MessageBookTest {
    private val now = 10_000_000L
    private fun chat(key: String, title: String, text: String, app: String = "WhatsApp", at: Long = now - 60_000, reply: Boolean = true, group: Boolean = false, from: String = title) =
        Chat(key, "pkg.$app", app, title, group, listOf(ChatLine(from, text, at)), reply)

    @Test fun `unread chats come newest first and reading marks them read`() {
        val b = MessageBook()
        b.put(chat("1", "Bro", "where are you", at = now - 5 * 60_000), now)
        b.put(chat("2", "Mom", "call me", at = now - 60_000), now)
        assertEquals(listOf("Mom", "Bro"), b.unread(now).map { it.title })
        b.markRead(listOf("2"))
        assertEquals(listOf("Bro"), b.unread(now).map { it.title })
        assertEquals("read chats are still there to reply to", 2, b.all(now).size)
    }

    @Test fun `a new message in a read chat makes it unread again, an identical update does not`() {
        val b = MessageBook()
        b.put(chat("1", "Bro", "hi"), now)
        b.markRead(listOf("1"))
        b.put(chat("1", "Bro", "hi"), now)           // the same notification refreshed
        assertTrue(b.unread(now).isEmpty())
        b.put(chat("1", "Bro", "are you there", at = now), now)
        assertEquals(1, b.unread(now).size)
    }

    @Test fun `people are found by name with forgiving matching`() {
        val b = MessageBook()
        b.put(chat("1", "Bro", "x"), now)
        b.put(chat("2", "Rahul Sharma", "y"), now)
        b.put(chat("3", "Rahul Verma", "z"), now)
        assertEquals(listOf("Bro"), b.from("bro", now).map { it.title })
        assertEquals(listOf("Rahul Sharma"), b.from("sharma", now).map { it.title })
        assertEquals(setOf("Rahul Sharma", "Rahul Verma"), b.from("Rahul", now).map { it.title }.toSet())
        assertEquals(listOf("Rahul Sharma"), b.from("rahul sharma", now).map { it.title })
        assertEquals("one wrong letter in a longer name", listOf("Rahul Sharma"), b.from("sharmo", now).map { it.title })
        assertTrue(b.from("nobody", now).isEmpty())
        assertTrue(b.from("", now).isEmpty())
    }

    @Test fun `a reply goes to a chat that offers a reply box, and an ambiguous name returns every candidate`() {
        val b = MessageBook()
        b.put(chat("1", "Bro", "x", reply = false, app = "Instagram"), now)
        b.put(chat("2", "Bro", "y", reply = true, app = "WhatsApp"), now)
        b.put(chat("3", "Mom", "z", reply = true, app = "WhatsApp", at = now - 1000), now)
        assertEquals(listOf("2"), b.replyTarget("bro", null, now).map { it.key })
        assertTrue("no reply box in that app, so no target", b.replyTarget("bro", "Instagram", now).isEmpty())
        assertEquals("with no name, the newest chat that can be replied to", listOf("3"), b.replyTarget(null, null, now).map { it.key })
        b.put(chat("4", "Bro", "w", reply = true, app = "Telegram"), now)
        assertEquals("the same name in two apps is a question for the owner", 2, b.replyTarget("bro", null, now).size)
        assertEquals(listOf("4"), b.replyTarget("bro", "Telegram", now).map { it.key })
    }

    @Test fun `old chats are forgotten and the book stays small`() {
        val b = MessageBook(maxChats = 3, maxAgeMs = 3600_000L)
        b.put(chat("old", "Old", "x", at = now - 2 * 3600_000), now)
        assertEquals("a two-hour-old message is gone", 0, b.size())
        for (i in 1..5) b.put(chat("k$i", "P$i", "m", at = now - i * 1000L), now)
        assertEquals(3, b.size())
        b.clear()
        assertEquals(0, b.size())
    }

    @Test fun `removing a notification removes the chat`() {
        val b = MessageBook()
        b.put(chat("1", "Bro", "x"), now)
        b.remove("1")
        assertEquals(0, b.size())
    }

    @Test fun `only the last few lines of a chat are kept`() {
        val b = MessageBook(maxLines = 3)
        val lines = (1..8).map { ChatLine("Bro", "m$it", now - (9 - it) * 1000L) }
        b.put(Chat("1", "p", "WhatsApp", "Bro", false, lines, true), now)
        assertEquals(listOf("m6", "m7", "m8"), b.all(now).first().lines.map { it.text })
    }

    @Test fun `what is read aloud names the app, shortens long text and caps the count`() {
        assertEquals("You have no new messages.", MessageBook.spoken(emptyList()))
        val one = MessageBook.spoken(listOf(chat("1", "Bro", "where are you")))
        assertEquals("You have one message. Bro on WhatsApp says: where are you.", one)
        val group = MessageBook.spoken(listOf(chat("2", "Family", "dinner at 8", group = true, from = "Dad")))
        assertTrue(group, group.contains("In Family on WhatsApp, Dad says: dinner at 8."))
        val many = MessageBook.spoken((1..6).map { chat("c$it", "P$it", "hello $it") })
        assertTrue(many, many.startsWith("You have 6 chats with messages."))
        assertTrue(many, many.endsWith("And 2 more."))
        assertFalse(many.contains("P5"))
        val long = MessageBook.spoken(listOf(chat("3", "Bro", "a".repeat(500))))
        assertTrue(long.length < 330)
        val earlier = MessageBook.spoken(listOf(Chat("4", "p", "Instagram", "Sam", false, listOf(ChatLine("Sam", "one", 1), ChatLine("Sam", "two", 2)), true)))
        assertTrue(earlier, earlier.contains("says: two and 1 earlier."))
    }
}
