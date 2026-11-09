package com.griffin.assistant.core

import java.util.Locale

/**
 * What the owner said, when it is something the phone can do by itself, without asking any model. Anything that
 * is not recognised becomes [Command.None] and goes to the assistant's brain instead.
 *
 * The parser is deliberately strict: a command only matches when it starts with a known verb, so ordinary
 * conversation ("call it a day", "I might set up a timer later") is never turned into an action.
 */
sealed class Command {
    data class Call(val who: String) : Command()
    data class Text(val who: String, val message: String) : Command()
    data class Timer(val seconds: Int) : Command()
    data class Alarm(val hour: Int, val minute: Int) : Command()
    data class Flashlight(val on: Boolean) : Command()
    data class OpenApp(val name: String) : Command()
    data class Volume(val up: Boolean) : Command()
    object Answer : Command()
    object Reject : Command()
    object Stop : Command()
    object None : Command()
}

object Commands {
    private val numberWords = mapOf(
        "a" to 1, "an" to 1, "one" to 1, "two" to 2, "three" to 3, "four" to 4, "five" to 5, "six" to 6, "seven" to 7, "eight" to 8,
        "nine" to 9, "ten" to 10, "eleven" to 11, "twelve" to 12, "fifteen" to 15, "twenty" to 20, "thirty" to 30, "forty" to 40,
        "forty five" to 45, "sixty" to 60, "half" to 0,
    )

    private fun clean(s: String) = s.lowercase(Locale.ROOT).replace(Regex("[,.!?]"), " ").replace(Regex("\\s+"), " ").trim()

    /** Words that make a contact name sound like a request rather than a name ("call it a day"). */
    private val notAName = setOf("it", "that", "this", "them", "me", "you", "back", "off", "out", "a", "the", "up", "down", "again")

    private val sentenceWords = setOf("is", "are", "was", "were", "has", "have", "will", "can", "should", "would", "could", "to", "of", "for", "and", "but", "if", "because", "that", "which", "when")

    fun parse(heard: String): Command {
        val s = clean(heard)
        if (s.isEmpty()) return Command.None

        // Call handling words, used while a call is ringing.
        when (s) {
            "answer", "answer it", "answer the call", "pick up", "pick it up", "pick up the call", "accept", "accept the call" -> return Command.Answer
            "reject", "reject it", "reject the call", "decline", "decline the call", "hang up", "ignore", "ignore it", "cut the call" -> return Command.Reject
            "stop", "be quiet", "quiet", "shut up", "cancel", "never mind", "nevermind" -> return Command.Stop
        }

        Regex("^(?:please )?(?:call|phone|ring|dial) (.+)$").find(s)?.let { m ->
            val who = m.groupValues[1].removePrefix("my ").trim()
            if (who.isNotEmpty() && who !in notAName && who.split(" ").first() !in notAName) return Command.Call(who)
        }

        Regex("^(?:please )?(?:text|message|sms|send (?:a )?(?:text|message|sms) to) ([a-z0-9' ]+?) (?:that|saying|say|to say|tell(?:ing)? (?:him|her|them)|:)\\s*(.+)$").find(s)?.let { m ->
            val who = m.groupValues[1].removePrefix("my ").trim()
            val body = m.groupValues[2].trim()
            if (who.isNotEmpty() && body.isNotEmpty() && who !in notAName) return Command.Text(who, body)
        }

        Regex("^(?:please )?(?:set (?:a )?|start (?:a )?)?timer(?: for)? (.+)$").find(s)?.let { m -> parseDuration(m.groupValues[1])?.let { return Command.Timer(it) } }
        Regex("^(?:please )?set (?:a )?(.+?) timer$").find(s)?.let { m -> parseDuration(m.groupValues[1])?.let { return Command.Timer(it) } }
        Regex("^(?:please )?(?:set|wake me)(?: an)? alarm(?: for| at)? (.+)$").find(s)?.let { m -> parseTime(m.groupValues[1])?.let { (h, mi) -> return Command.Alarm(h, mi) } }
        Regex("^(?:please )?wake me (?:up )?at (.+)$").find(s)?.let { m -> parseTime(m.groupValues[1])?.let { (h, mi) -> return Command.Alarm(h, mi) } }

        when (s) {
            "flashlight on", "turn on the flashlight", "turn the flashlight on", "torch on", "turn on the torch", "turn on flashlight" -> return Command.Flashlight(true)
            "flashlight off", "turn off the flashlight", "turn the flashlight off", "torch off", "turn off the torch", "turn off flashlight" -> return Command.Flashlight(false)
            "volume up", "turn it up", "turn the volume up", "louder", "increase the volume" -> return Command.Volume(true)
            "volume down", "turn it down", "turn the volume down", "quieter", "lower the volume", "decrease the volume" -> return Command.Volume(false)
        }

        Regex("^(?:please )?(?:open|launch|start) ([a-z0-9' ]+)$").find(s)?.let { m ->
            val app = m.groupValues[1].removePrefix("the ").removeSuffix(" app").trim()
            val words = app.split(" ")
            // App names are short and are names, not sentences: "open source is great" is not a request to open an app.
            if (app.isNotEmpty() && words.size <= 3 && words.none { it in sentenceWords } && app !in setOf("timer", "a timer", "alarm", "an alarm")) return Command.OpenApp(app)
        }
        return Command.None
    }

    /** "5 minutes", "ten minutes", "an hour", "90 seconds", "1 hour 30 minutes", "half an hour". */
    fun parseDuration(text: String): Int? {
        val s = clean(text)
        if (s == "half an hour" || s == "half hour") return 1800
        var total = 0
        var found = false
        for (m in Regex("(\\d+|[a-z]+(?: [a-z]+)?) (hours?|hrs?|minutes?|mins?|seconds?|secs?)").findAll(s)) {
            val n = m.groupValues[1].toIntOrNull() ?: numberWords[m.groupValues[1]] ?: continue
            total += n * when {
                m.groupValues[2].startsWith("h") -> 3600
                m.groupValues[2].startsWith("m") -> 60
                else -> 1
            }
            found = true
        }
        return if (found && total in 1..(24 * 3600)) total else null
    }

    /** "7", "7 am", "7:30 pm", "6 30", "noon". Without am or pm the hour is taken as said. */
    fun parseTime(text: String): Pair<Int, Int>? {
        val s = clean(text).replace(".", "")
        if (s == "noon") return 12 to 0
        if (s == "midnight") return 0 to 0
        val m = Regex("^(\\d{1,2})(?:[: ](\\d{2}))?\\s*(am|pm|a m|p m)?$").find(s) ?: return null
        var h = m.groupValues[1].toInt()
        val min = m.groupValues[2].ifEmpty { "0" }.toInt()
        val mer = m.groupValues[3].replace(" ", "")
        if (h > 23 || min > 59) return null
        when {
            mer == "pm" && h in 1..11 -> h += 12
            mer == "am" && h == 12 -> h = 0
            // With no am or pm the hour is taken as said ("7" is 07:00); the confirmation speaks the am or pm so a wrong guess is heard at once.
        }
        return h to min
    }
}
