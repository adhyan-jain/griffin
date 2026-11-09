package com.griffin.assistant.core

import java.util.Locale

/** One line of a chat. */
data class ChatLine(val from: String, val text: String, val at: Long)

/**
 * A conversation that currently has a notification: who it is with, in which app, the last few lines, and whether the
 * notification offers a "Reply" box (which is how a reply is sent without opening the app).
 */
data class Chat(val key: String, val app: String, val appName: String, val title: String, val group: Boolean, val lines: List<ChatLine>, val canReply: Boolean, val read: Boolean = false) {
    val lastAt get() = lines.maxOfOrNull { it.at } ?: 0L
}

/**
 * The messages the assistant can read out and reply to. It holds only what is in the phone's notifications right now,
 * in memory: nothing is written to disk, nothing is sent anywhere, old chats are forgotten after [maxAgeMs], and
 * only [maxChats] are kept. Which chats exist is decided by the notifications; this class only organises them.
 */
class MessageBook(private val maxChats: Int = 60, private val maxAgeMs: Long = 2 * 3600_000L, private val maxLines: Int = 5) {
    private val chats = LinkedHashMap<String, Chat>()

    /** Adds or updates a conversation. A notification for a chat that was already read makes it unread again. */
    @Synchronized fun put(chat: Chat, now: Long) {
        val prev = chats[chat.key]
        val unchanged = prev != null && prev.lines.lastOrNull() == chat.lines.lastOrNull()
        chats[chat.key] = chat.copy(lines = chat.lines.takeLast(maxLines), read = unchanged && prev!!.read)
        prune(now)
    }

    @Synchronized fun remove(key: String) { chats.remove(key) }
    @Synchronized fun clear() = chats.clear()
    @Synchronized fun size() = chats.size

    private fun prune(now: Long) {
        chats.entries.removeIf { now - it.value.lastAt > maxAgeMs }
        while (chats.size > maxChats) chats.remove(chats.keys.first())
    }

    @Synchronized fun unread(now: Long): List<Chat> { prune(now); return chats.values.filter { !it.read }.sortedByDescending { it.lastAt } }

    @Synchronized fun all(now: Long): List<Chat> { prune(now); return chats.values.sortedByDescending { it.lastAt } }

    @Synchronized fun markRead(keys: Collection<String>) { keys.forEach { k -> chats[k]?.let { chats[k] = it.copy(read = true) } } }

    /** Chats with the person or group named, best match first. Matching ignores case, punctuation and one wrong letter. */
    @Synchronized fun from(who: String, now: Long): List<Chat> {
        prune(now)
        return chats.values.map { it to score(who, it) }.filter { it.second > 0 }.sortedWith(compareByDescending<Pair<Chat, Int>> { it.second }.thenByDescending { it.first.lastAt }).map { it.first }
    }

    /** A chat that can be replied to: the person's, or (with no name) the one most recently read or received. */
    @Synchronized fun replyTarget(who: String?, appName: String?, now: Long): List<Chat> {
        prune(now)
        val pool = chats.values.filter { it.canReply && (appName == null || it.appName.equals(appName, true)) }
        if (who.isNullOrBlank()) return pool.sortedByDescending { it.lastAt }.take(1)
        val scored = pool.map { it to score(who, it) }.filter { it.second > 0 }
        val best = scored.maxOfOrNull { it.second } ?: return emptyList()
        return scored.filter { it.second == best }.map { it.first }.sortedByDescending { it.lastAt }
    }

    private fun norm(s: String) = s.lowercase(Locale.ROOT).replace(Regex("[^\\p{L}\\p{N} ]"), "").trim()

    /** 3 exact name, 2 every word of the query is in the name, 1 a single word differs by one letter. */
    private fun score(query: String, chat: Chat): Int {
        val q = norm(query)
        val name = norm(chat.title)
        if (q.isEmpty()) return 0
        if (q == name) return 3
        val qWords = q.split(" ").filter { it.isNotEmpty() }
        val nWords = name.split(" ").filter { it.isNotEmpty() }
        if (qWords.all { it in nWords }) return 2
        if (qWords.size == 1 && qWords[0].length >= 4 && nWords.any { WakeMatcher.levenshtein(it, qWords[0]) <= 1 }) return 1
        return 0
    }

    companion object {
        /** What is said aloud for a set of chats, newest first: short, with the app named, never more than [limit] chats. */
        fun spoken(chats: List<Chat>, limit: Int = 4): String {
            if (chats.isEmpty()) return "You have no new messages."
            val shown = chats.take(limit)
            val intro = if (chats.size == 1) "You have one message." else "You have ${chats.size} chats with messages."
            val parts = shown.map { c ->
                val last = c.lines.last()
                val said = clip(last.text, 220)
                val more = if (c.lines.size > 1) " and ${c.lines.size - 1} earlier" else ""
                if (c.group) "In ${c.title} on ${c.appName}, ${last.from} says: $said$more." else "${c.title} on ${c.appName} says: $said$more."
            }
            val rest = if (chats.size > limit) " And ${chats.size - limit} more." else ""
            return (listOf(intro) + parts).joinToString(" ") + rest
        }

        private fun clip(s: String, n: Int) = if (s.length <= n) s else s.take(n).trimEnd() + "…"
    }
}
