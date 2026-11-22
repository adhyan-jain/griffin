package com.griffin.assistant.core

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import java.util.concurrent.CountDownLatch
import kotlin.concurrent.thread

class OutboxTest {
    private fun notice(seq: Long, entity: String = "e$seq", kind: String = "task") =
        OutboxNotice("id$seq", seq, kind, entity, "t$seq", "b$seq", "NORMAL", "https://app/tasks/$entity")

    private class Phone(var cursor: Long? = null) {
        val shown = mutableListOf<OutboxNotice>()
        val acked = mutableListOf<String>()
    }

    private fun poller(phone: Phone, server: (String) -> OutboxPage, ackFails: Boolean = false) = OutboxPoller(
        load = { phone.cursor },
        save = { phone.cursor = it },
        fetch = server,
        show = { phone.shown.add(it) },
        ack = { if (ackFails) throw RuntimeException("offline") else phone.acked.addAll(it) },
    )

    @Test fun `a fresh install starts at the latest cursor and shows nothing old`() {
        val phone = Phone()
        val asked = mutableListOf<String>()
        val shown = poller(phone, { after -> asked.add(after); OutboxPage(42, listOf(notice(41), notice(42))) }).poll()
        assertEquals(0, shown)
        assertEquals(listOf("latest"), asked)
        assertEquals(42L, phone.cursor)
        assertTrue(phone.shown.isEmpty())
    }

    @Test fun `each notice is shown exactly once however often the phone polls`() {
        val phone = Phone(cursor = 10)
        val all = listOf(notice(11), notice(12))
        val server = { after: String -> OutboxPage(12, all.filter { it.seq > after.toLong() }) }
        val p = poller(phone, server)
        repeat(5) { p.poll() }
        assertEquals(listOf(11L, 12L), phone.shown.map { it.seq })
        assertEquals(listOf("id11", "id12"), phone.acked)
        assertEquals(12L, phone.cursor)
    }

    @Test fun `a server that resends old rows cannot make them show again`() {
        val phone = Phone(cursor = 12)
        poller(phone, { OutboxPage(12, listOf(notice(11), notice(12))) }).poll()
        assertTrue(phone.shown.isEmpty())
    }

    @Test fun `a failed ack still moves the cursor so nothing repeats`() {
        val phone = Phone(cursor = 1)
        val p = poller(phone, { after -> OutboxPage(2, listOf(notice(2)).filter { it.seq > after.toLong() }) }, ackFails = true)
        p.poll(); p.poll()
        assertEquals(1, phone.shown.size)
        assertEquals(2L, phone.cursor)
    }

    @Test fun `two polls never run at once`() {
        val phone = Phone(cursor = 0)
        val inside = CountDownLatch(1)
        val release = CountDownLatch(1)
        val p = poller(phone, { inside.countDown(); release.await(); OutboxPage(1, listOf(notice(1))) })
        val first = thread { p.poll() }
        inside.await()
        assertEquals(-1, p.poll())
        release.countDown(); first.join()
        assertEquals(1, phone.shown.size)
    }

    @Test fun `updates about the same thing replace each other and only approvals wake the screen`() {
        assertEquals(notificationIdFor(notice(1, entity = "task-a")), notificationIdFor(notice(2, entity = "task-a")))
        assertNotEquals(notificationIdFor(notice(1, entity = "task-a")), notificationIdFor(notice(2, entity = "task-b")))
        assertTrue(wakesScreen(notice(1, kind = "approval")))
        assertEquals(false, wakesScreen(notice(1, kind = "task")))
    }
}
