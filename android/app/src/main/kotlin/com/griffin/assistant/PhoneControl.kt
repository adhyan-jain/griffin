package com.griffin.assistant

import org.json.JSONObject

/** One phone-local job from JJ chat: an alarm, a timer, opening an app or a link. Safe to run without asking: it only touches this phone. */
data class PhoneControl(
    val action: String,
    val hour: Int = 0,
    val minute: Int = 0,
    val seconds: Int = 0,
    val label: String = "",
    val days: List<Int> = emptyList(),
    val name: String = "",
    val url: String = "",
)

/** What the phone does for a [PhoneControl]. [Actions] is the real one; tests use a fake. Each returns the sentence for the owner. */
interface PhoneControlOps {
    fun alarm(hour: Int, minute: Int, label: String, days: List<Int>): String
    fun timer(seconds: Int, label: String): String
    fun openApp(name: String): String
    fun openUrl(url: String): String
}

class ActionsPhoneControl(private val actions: Actions) : PhoneControlOps {
    override fun alarm(hour: Int, minute: Int, label: String, days: List<Int>) = actions.alarm(hour, minute, label, days)
    override fun timer(seconds: Int, label: String) = actions.timer(seconds, label)
    override fun openApp(name: String) = actions.openApp(name)
    override fun openUrl(url: String) = actions.openUrl(url)
}

val PHONE_CONTROL_ACTIONS = setOf("set_alarm", "set_timer", "open_app", "open_url")

/** Reads the task's `phone_control` object, or null when the task is not one. Bad values become a [PhoneControl] that [runPhoneControl] refuses. */
fun parsePhoneControl(o: JSONObject?): PhoneControl? {
    val action = o?.optString("action").orEmpty()
    if (o == null || action !in PHONE_CONTROL_ACTIONS) return null
    val days = o.optJSONArray("days")?.let { a -> (0 until a.length()).map { a.optInt(it, -1) } } ?: emptyList()
    return PhoneControl(
        action = action, hour = o.optInt("hour", -1), minute = o.optInt("minute", -1), seconds = o.optInt("seconds", 0),
        label = o.optString("label").trim(), days = days, name = o.optString("name").trim(), url = o.optString("url").trim(),
    )
}

/** Outcome of a phone-local job: [ok] false means fail the task with [text]. */
data class PhoneControlResult(val ok: Boolean, val text: String)

fun runPhoneControl(c: PhoneControl, ops: PhoneControlOps): PhoneControlResult {
    fun done(text: String, success: String) = PhoneControlResult(text.startsWith(success), text)
    return when (c.action) {
        "set_alarm" -> when {
            c.hour !in 0..23 || c.minute !in 0..59 -> PhoneControlResult(false, "That alarm time is not valid.")
            c.days.any { it !in 1..7 } -> PhoneControlResult(false, "Alarm days must be 1 to 7.")
            else -> done(ops.alarm(c.hour, c.minute, c.label, c.days.distinct()), "Alarm set")
        }
        "set_timer" -> if (c.seconds !in 1..86_400) PhoneControlResult(false, "A timer must be between 1 second and 24 hours.")
        else done(ops.timer(c.seconds, c.label), "Timer set")
        "open_app" -> if (c.name.isBlank()) PhoneControlResult(false, "No app name was given.")
        else done(ops.openApp(c.name), "Opening")
        "open_url" -> if (!Actions.isWebUrl(c.url)) PhoneControlResult(false, "Only http and https links can be opened.")
        else done(ops.openUrl(c.url), "Opening")
        else -> PhoneControlResult(false, "Unknown phone action.")
    }
}
