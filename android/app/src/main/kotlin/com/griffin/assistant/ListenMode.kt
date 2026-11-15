package com.griffin.assistant

import android.content.Context

/**
 * How the assistant is woken. Two ways, and the owner picks:
 *  - wake word (the default): the microphone listens all the time for "hey <name>", and the ball, button and tile also work;
 *  - tap only: the microphone stays off. It opens for one utterance when the ball, the button, the notification or
 *    the tile is tapped, then closes again. No wake-word false alarms and no listening noise.
 */
object ListenMode {
    fun wakeWord(context: Context): Boolean = context.getSharedPreferences("griffin", Context.MODE_PRIVATE).getBoolean("wake_word", true)

    fun setWakeWord(context: Context, on: Boolean) {
        context.getSharedPreferences("griffin", Context.MODE_PRIVATE).edit().putBoolean("wake_word", on).apply()
    }
}
