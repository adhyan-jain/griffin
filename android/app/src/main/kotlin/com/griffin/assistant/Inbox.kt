package com.griffin.assistant

import android.app.Notification
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.os.Bundle
import android.app.RemoteInput
import com.griffin.assistant.core.MessageBook

/**
 * The phone's current message notifications, kept in memory only, and the way to reply to them. A reply goes through
 * the same "Reply" box the notification itself offers, so the other app (WhatsApp, Instagram, Messenger ...) does the
 * sending and nothing is opened on screen: a game or a video keeps running.
 */
object Inbox {
    val book = MessageBook()

    /** The reply action of each notification, by key. PendingIntents cannot be saved, and are not. */
    private val actions = HashMap<String, Notification.Action>()

    @Synchronized fun remember(key: String, action: Notification.Action?) { if (action != null) actions[key] = action else actions.remove(key) }
    @Synchronized fun forget(key: String) { actions.remove(key); book.remove(key) }
    @Synchronized fun clear() { actions.clear(); book.clear() }

    /** Sends [text] as a reply to the chat with this notification key. Returns null on success, or what to tell the owner. */
    @Synchronized fun reply(context: Context, key: String, text: String): String? {
        val action = actions[key] ?: return "That chat is no longer available to reply to."
        val inputs = action.remoteInputs ?: return "That chat cannot be replied to from here."
        return try {
            val intent = Intent()
            val results = Bundle().apply { inputs.forEach { putCharSequence(it.resultKey, text) } }
            RemoteInput.addResultsToIntent(inputs, intent, results)
            action.actionIntent.send(context, 0, intent)
            null
        } catch (e: PendingIntent.CanceledException) {
            "That chat was closed before I could reply."
        } catch (e: Exception) {
            android.util.Log.w("Inbox", "reply failed: ${e.javaClass.simpleName}")
            "I could not send that reply (${e.javaClass.simpleName.removeSuffix("Exception").lowercase().ifEmpty { "error" }})."
        }
    }
}
