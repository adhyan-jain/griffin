package com.griffin.assistant.core

import java.util.Locale

/**
 * The wake phrase is "hey" followed by the manager's name. The name alone does not wake the assistant: a
 * three-letter name said in any sentence would otherwise trigger it constantly.
 *
 * Finds that phrase at the start of what was heard and returns what follows it.
 *
 * A speech recogniser does not know the name, so it mishears it: "JJ" arrives as "J J", "jay jay" or "Jay-Jay",
 * and "Jarvis" as "Jarvis" or "Jarvas". The matcher therefore compares sound-alike forms, not exact text:
 * letters only, lower case, spaces removed, and for longer names it tolerates one wrong letter. Short names
 * (three letters or fewer) must match exactly, because one wrong letter in a short word is a different word.
 *
 * Only the START of an utterance counts ("jarvis call mom", "hey jarvis, call mom"), so a sentence that merely
 * mentions the name ("tell Jarvis I said hi") does not wake the assistant.
 */
object WakeMatcher {
    /** What was heard after the name, or "" when only the name was said. */
    data class Wake(val rest: String)

    /** How a recogniser writes "hey". Only these start the wake phrase. */
    private val heys = setOf("hey", "hay", "hei", "heya", "hae")

    /** How a letter-name sounds when spoken: "jj" can be heard as "jay jay". */
    private val spokenLetters = mapOf(
        'a' to "ay", 'b' to "bee", 'c' to "see", 'd' to "dee", 'e' to "ee", 'f' to "ef", 'g' to "gee", 'h' to "aitch",
        'i' to "eye", 'j' to "jay", 'k' to "kay", 'l' to "el", 'm' to "em", 'n' to "en", 'o' to "oh", 'p' to "pee",
        'q' to "cue", 'r' to "ar", 's' to "ess", 't' to "tee", 'u' to "you", 'v' to "vee", 'w' to "doubleyou", 'x' to "ex",
        'y' to "why", 'z' to "zed",
    )

    private fun norm(s: String): String = s.lowercase(Locale.ROOT).filter { it.isLetterOrDigit() }

    /** All the ways the name may be written by a recogniser: as typed, and with each letter spelled out. */
    fun forms(name: String): Set<String> {
        val base = norm(name)
        if (base.isEmpty()) return emptySet()
        val out = mutableSetOf(base)
        if (base.length <= 3 && base.all { it.isLetter() }) {
            out += base.map { spokenLetters[it] ?: it.toString() }.joinToString("")
        }
        return out
    }

    fun match(heard: String, name: String): Wake? {
        val forms = forms(name)
        if (forms.isEmpty()) return null
        val words = heard.trim().split(Regex("\\s+")).filter { it.isNotEmpty() }
        if (words.isEmpty()) return null
        if (norm(words[0]) !in heys) return null // "hey" is required
        val start = 1
        // The name is one to four words ("J J", "jay jay", "Mr Stark"); try the shortest span that matches.
        val maxSpan = minOf(4, words.size - start)
        if (maxSpan <= 0) return null
        for (span in 1..maxSpan) {
            val said = norm(words.subList(start, start + span).joinToString(""))
            if (forms.any { sounds(said, it) }) {
                val rest = words.drop(start + span).joinToString(" ").trim().trimStart(',', ':', '-', '.', '!', '?').trim()
                return Wake(rest)
            }
        }
        return null
    }

    private fun sounds(said: String, form: String): Boolean {
        if (said == form) return true
        if (form.length <= 3) return false // one wrong letter in a short word is another word
        return levenshtein(said, form) <= 1
    }

    fun levenshtein(a: String, b: String): Int {
        if (a == b) return 0
        if (a.isEmpty()) return b.length
        if (b.isEmpty()) return a.length
        var prev = IntArray(b.length + 1) { it }
        for (i in 1..a.length) {
            val cur = IntArray(b.length + 1)
            cur[0] = i
            for (j in 1..b.length) {
                val cost = if (a[i - 1] == b[j - 1]) 0 else 1
                cur[j] = minOf(cur[j - 1] + 1, prev[j] + 1, prev[j - 1] + cost)
            }
            prev = cur
        }
        return prev[b.length]
    }
}
