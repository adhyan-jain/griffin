package com.griffin.assistant.core

import java.util.Locale

/**
 * The owner's aliases ("mummy" means "mom"), kept on the server and mirrored here so the commands the phone handles
 * itself (call, message, open a chat) understand them too. Whole words only, longest alias first, and text that was
 * just inserted is never scanned again. The server applies the same rule to everything else.
 */
object Aliases {
    private val token = Regex("[\\p{L}\\p{N}]+(?:['.-][\\p{L}\\p{N}]+)*")

    fun apply(text: String, aliases: Map<String, String>): String {
        if (aliases.isEmpty()) return text
        val maxWords = aliases.keys.maxOf { it.trim().split(Regex("\\s+")).size }
        val toks = token.findAll(text).toList()
        val out = StringBuilder()
        var last = 0
        var i = 0
        while (i < toks.size) {
            var done = false
            var n = minOf(maxWords, toks.size - i)
            while (n >= 1 && !done) {
                val from = toks[i].range.first
                val to = toks[i + n - 1].range.last + 1
                val span = text.substring(from, to)
                val single = n == 1 || span.split(Regex("\\s+")).joinToString(" ") == span
                val target = if (single) aliases[span.lowercase(Locale.ROOT)] else null
                if (target != null) {
                    out.append(text, last, from).append(target)
                    last = to
                    i += n
                    done = true
                }
                n--
            }
            if (!done) i++
        }
        return out.append(text, last, text.length).toString()
    }
}
