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
    /** Read out new messages, from one person or from everyone. */
    data class ReadMessages(val from: String?) : Command()
    /** Reply to the chat just read (who is null) or to a named person, optionally in a named app. */
    data class Reply(val text: String, val who: String? = null, val app: String? = null) : Command()
    /** Message someone in a named app ("message bro on whatsapp saying ..."). */
    data class MessageVia(val who: String, val text: String, val app: String) : Command()
    data class Timer(val seconds: Int) : Command()
    data class Alarm(val hour: Int, val minute: Int) : Command()
    data class Flashlight(val on: Boolean? = null) : Command()
    data class OpenApp(val name: String) : Command()
    data class Volume(val up: Boolean) : Command()
    data class Mute(val mute: Boolean) : Command()
    object Battery : Command()
    object Time : Command()
    object Date : Command()
    object Camera : Command()
    object MediaNext : Command()
    object MediaPrevious : Command()
    object MediaPlay : Command()
    object MediaPause : Command()
    object MediaPlayPause : Command()
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

    /** The name an app goes by, as the owner says it. */
    fun appName(said: String): String? = when (said.trim().lowercase(Locale.ROOT)) {
        "whatsapp" -> "WhatsApp"
        "instagram", "insta" -> "Instagram"
        "messenger" -> "Messenger"
        "telegram" -> "Telegram"
        "signal" -> "Signal"
        "sms" -> "Messages"
        else -> null
    }

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
            "stop", "be quiet", "quiet", "shut up", "cancel", "never mind", "nevermind", "that's all", "thats all", "that is all", "thanks", "thank you", "okay thanks", "ok thanks", "no thanks", "i'm done", "done" -> return Command.Stop
        }

        Regex("^(?:please )?(?:call|phone|ring|dial) (.+)$").find(s)?.let { m ->
            val who = m.groupValues[1].removePrefix("my ").trim()
            if (who.isNotEmpty() && who !in notAName && who.split(" ").first() !in notAName) return Command.Call(who)
        }

        // Reading and replying to chats (the apps' own notifications), and messaging in a named app.
        Regex("^(?:please )?(?:read|check)(?: out)?(?: me)?(?: my| the)?(?: new| unread)? (?:messages?|texts?|chats?|dms?|notifications?)(?: from (.+))?$").find(s)?.let { m -> return Command.ReadMessages(m.groupValues[1].ifEmpty { null }?.removePrefix("my ")) }
        Regex("^(?:do i have |are there |any |have i got )(?:any )?(?:new |unread )?(?:messages?|texts?|dms?)(?: from (.+))?$").find(s)?.let { m -> return Command.ReadMessages(m.groupValues[1].ifEmpty { null }?.removePrefix("my ")) }
        Regex("^what did (.+?) (?:say|text|send|write)$").find(s)?.let { m -> return Command.ReadMessages(m.groupValues[1].removePrefix("my ")) }
        Regex("^read (?:me )?(.+?)'?s? (?:messages?|texts?|chats?)$").find(s)?.let { m -> return Command.ReadMessages(m.groupValues[1].removePrefix("my ")) }
        Regex("^(?:please )?reply(?: to (.+?))?(?: (?:on|in|via) (whatsapp|instagram|insta|messenger|telegram|signal|sms))?(?: saying| with| that| say)?:? (.+)$").find(s)?.let { m ->
            val text = m.groupValues[3].trim()
            if (text.isNotEmpty()) return Command.Reply(text, m.groupValues[1].ifEmpty { null }?.removePrefix("my "), appName(m.groupValues[2]))
        }
        Regex("^(?:please )?(?:message|dm|whatsapp|text|tell|send (?:a )?(?:message |text |dm )?to) ([a-z0-9' ]+?) (?:on|in|via|using) (whatsapp|instagram|insta|messenger|telegram|signal|sms)(?: that| saying| say| to say|:)\\s*(.+)$").find(s)?.let { m ->
            val who = m.groupValues[1].removePrefix("my ").trim()
            val text = m.groupValues[3].trim()
            if (who.isNotEmpty() && text.isNotEmpty() && who !in notAName) return Command.MessageVia(who, text, appName(m.groupValues[2])!!)
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

        // Torch / Flashlight: on, off, toggle, and Hindi colloquial triggers
        when (s) {
            "torch on", "turn on torch", "turn on the torch", "turn torch on", "torch chalu karo", "torch on karo", "torch jalao", "torch chalao",
            "flashlight on", "turn on flashlight", "turn on the flashlight", "turn flashlight on", "flashlight chalu karo", "flashlight on karo",
            "light on", "turn on light", "turn on the light", "turn light on",
            "switch on torch", "switch on the torch", "switch on flashlight", "switch on the flashlight",
            "enable torch", "enable flashlight" -> return Command.Flashlight(true)

            "torch off", "turn off torch", "turn off the torch", "turn torch off", "torch band karo", "torch off karo", "torch bujhao",
            "flashlight off", "turn off flashlight", "turn off the flashlight", "turn flashlight off", "flashlight band karo", "flashlight off karo",
            "light off", "turn off light", "turn off the light", "turn light off",
            "switch off torch", "switch off the torch", "switch off flashlight", "switch off the flashlight",
            "disable torch", "disable flashlight" -> return Command.Flashlight(false)

            "torch", "flashlight", "toggle torch", "toggle flashlight", "torch light" -> return Command.Flashlight(null)

            // Battery status
            "battery", "battery level", "battery percentage", "battery status", "how much battery", "check battery",
            "what is my battery", "what's my battery", "whats my battery", "battery kitni hai", "battery percent" -> return Command.Battery

            // Time & Date
            "what time is it", "what's the time", "whats the time", "tell me the time", "current time", "what is the time",
            "kya time hua", "time kya hai" -> return Command.Time

            "what date is it", "what's the date", "whats the date", "what is today's date", "today's date", "todays date",
            "what day is it", "today" -> return Command.Date

            // Quick Camera
            "camera", "open camera", "take a photo", "take a picture", "take a selfie", "snap a photo", "click a photo", "take photo", "take picture" -> return Command.Camera

            // Volume & Mute
            "volume up", "turn it up", "turn the volume up", "louder", "increase the volume", "increase volume" -> return Command.Volume(true)
            "volume down", "turn it down", "turn the volume down", "quieter", "lower the volume", "decrease the volume", "decrease volume" -> return Command.Volume(false)
            "mute", "mute phone", "mute volume", "silence", "silent", "mute audio" -> return Command.Mute(true)
            "unmute", "unmute phone", "unmute volume", "unmute audio" -> return Command.Mute(false)

            // Media playback
            "next song", "next track", "skip song", "skip track", "skip", "change song", "change the song", "change spotify song", "spotify next", "next" -> return Command.MediaNext
            "previous song", "previous track", "prev song", "prev track", "last song", "replay song" -> return Command.MediaPrevious
            "play music", "play song", "resume music", "resume song", "play spotify", "resume spotify", "play", "resume" -> return Command.MediaPlay
            "pause music", "pause song", "stop music", "pause spotify", "pause" -> return Command.MediaPause
            "play pause", "toggle music", "toggle playback" -> return Command.MediaPlayPause

            // Common standalone app names
            "calculator", "calc" -> return Command.OpenApp("calculator")
            "settings" -> return Command.OpenApp("settings")
            "gallery", "photos" -> return Command.OpenApp("photos")
            "clock" -> return Command.OpenApp("clock")
            "calendar" -> return Command.OpenApp("calendar")
            "maps", "google maps" -> return Command.OpenApp("maps")
            "spotify" -> return Command.OpenApp("spotify")
            "youtube" -> return Command.OpenApp("youtube")
            "whatsapp" -> return Command.OpenApp("whatsapp")
            "instagram", "insta" -> return Command.OpenApp("instagram")
            "chrome", "browser" -> return Command.OpenApp("chrome")
            "files", "file manager" -> return Command.OpenApp("files")
            "gmail", "email", "mail" -> return Command.OpenApp("gmail")
        }

        // Open/Launch any installed app: English & Hindi
        Regex("^(?:please )?(?:open(?: up)?|launch|start|run|show|go to|switch to) ([a-z0-9' ]+)$").find(s)?.let { m ->
            val app = m.groupValues[1].removePrefix("the ").removeSuffix(" app").trim()
            val words = app.split(" ")
            if (app.isNotEmpty() && words.size <= 3 && words.none { it in sentenceWords } && app !in setOf("timer", "a timer", "alarm", "an alarm")) return Command.OpenApp(app)
        }
        Regex("^([a-z0-9' ]+?) (?:kholo|chalao|open karo|start karo)$").find(s)?.let { m ->
            val app = m.groupValues[1].removePrefix("the ").removeSuffix(" app").trim()
            val words = app.split(" ")
            if (app.isNotEmpty() && words.size <= 3 && words.none { it in sentenceWords } && app !in setOf("timer", "a timer", "alarm", "an alarm")) return Command.OpenApp(app)
        }
        Regex("^(?:kholo|chalao) ([a-z0-9' ]+)$").find(s)?.let { m ->
            val app = m.groupValues[1].removePrefix("the ").removeSuffix(" app").trim()
            val words = app.split(" ")
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
