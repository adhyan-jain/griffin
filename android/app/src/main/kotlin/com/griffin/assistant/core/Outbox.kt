package com.griffin.assistant.core

import java.util.concurrent.atomic.AtomicBoolean

/** One row of Griffin's notification outbox: an event the owner should hear about, written once by the server. */
data class OutboxNotice(
    val id: String,
    val seq: Long,
    val kind: String,
    val entityId: String,
    val title: String,
    val body: String,
    val priority: String,
    val actionUrl: String,
)

/** What the server returned: the rows after the cursor that was sent, and the cursor to send next time. */
data class OutboxPage(val cursor: Long, val items: List<OutboxNotice>)

/**
 * Shows each outbox row on the phone exactly once. The phone remembers a cursor (the last seq it showed), not a
 * set of ids, so nothing old can come back: a fresh install starts at the server's latest cursor and replays
 * nothing, and a poll only ever shows rows with a seq above the cursor. Only one poll runs at a time.
 */
class OutboxPoller(
    private val load: () -> Long?,
    private val save: (Long) -> Unit,
    private val fetch: (after: String) -> OutboxPage,
    private val show: (OutboxNotice) -> Unit,
    private val ack: (ids: List<String>) -> Unit,
) {
    private val running = AtomicBoolean(false)

    /** Returns how many notices were shown, or -1 when another poll is still running. */
    fun poll(): Int {
        if (!running.compareAndSet(false, true)) return -1
        try {
            val cursor = load()
            if (cursor == null) {
                save(fetch("latest").cursor)
                return 0
            }
            val page = fetch(cursor.toString())
            val fresh = page.items.filter { it.seq > cursor }.sortedBy { it.seq }
            var shown = 0
            for (n in fresh) {
                try { show(n); shown++ } catch (_: Exception) { /* one bad row never blocks the rest */ }
            }
            if (fresh.isNotEmpty()) {
                try { ack(fresh.map { it.id }) } catch (_: Exception) { /* the cursor alone prevents repeats */ }
            }
            val next = maxOf(page.cursor, fresh.maxOfOrNull { it.seq } ?: cursor)
            if (next > cursor) save(next)
            return shown
        } finally {
            running.set(false)
        }
    }
}

/** A stable Android notification id per thing, so a newer event about it replaces the old one instead of stacking. */
fun notificationIdFor(n: OutboxNotice): Int = n.entityId.ifEmpty { n.id }.hashCode()

/** Only an approval (something waiting for the owner) is worth lighting up the screen for. */
fun wakesScreen(n: OutboxNotice): Boolean = n.kind == "approval"
