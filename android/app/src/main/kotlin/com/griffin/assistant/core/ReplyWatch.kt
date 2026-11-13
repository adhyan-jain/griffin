package com.griffin.assistant.core

/**
 * Recognises a chat notification from someone the owner is waiting to hear from (an outreach campaign), so only
 * those replies are reported to Griffin. The names come from the server; every other notification stays on the
 * phone. Matching is exact on the whole name, ignoring case and spacing, so "Sam" never matches "Samantha".
 */
class ReplyWatch {
    @Volatile private var names: Set<String> = emptySet()

    fun update(awaiting: Collection<String>) { names = awaiting.map(::norm).filter { it.isNotEmpty() }.toSet() }

    fun matches(chatTitle: String): Boolean = norm(chatTitle).let { it.isNotEmpty() && it in names }

    private fun norm(s: String) = s.trim().lowercase().replace(Regex("\\s+"), " ")
}
