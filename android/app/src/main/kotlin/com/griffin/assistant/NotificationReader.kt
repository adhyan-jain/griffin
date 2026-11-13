package com.griffin.assistant

import android.app.Notification
import android.os.Bundle
import android.service.notification.NotificationListenerService
import android.service.notification.StatusBarNotification
import com.griffin.assistant.core.Chat
import com.griffin.assistant.core.ChatLine

/**
 * Lets the assistant read out, and reply to, the chat notifications that are on the phone. It only looks at message
 * notifications (a chat app, or a notification marked as a message), keeps them in memory, and never logs them.
 * Android only runs this after the owner turns on "Notification access" for Griffin in the phone's settings.
 */
class NotificationReader : NotificationListenerService() {
    private val app get() = application as GriffinApp

    override fun onListenerConnected() {
        runCatching { activeNotifications?.forEach(::ingest) }
    }

    override fun onListenerDisconnected() = Inbox.clear()

    override fun onNotificationPosted(sbn: StatusBarNotification) = ingest(sbn)

    override fun onNotificationRemoved(sbn: StatusBarNotification) = Inbox.forget(sbn.key)

    private fun ingest(sbn: StatusBarNotification) {
        val n = sbn.notification ?: return
        if (sbn.packageName == packageName || (n.flags and Notification.FLAG_GROUP_SUMMARY) != 0) return
        val known = sbn.packageName in MESSAGING_APPS
        if (n.category != Notification.CATEGORY_MESSAGE && !known) return
        val extras = n.extras ?: return

        val title = (extras.getCharSequence(Notification.EXTRA_CONVERSATION_TITLE) ?: extras.getCharSequence(Notification.EXTRA_TITLE))?.toString()?.trim().orEmpty()
        val group = extras.getBoolean(Notification.EXTRA_IS_GROUP_CONVERSATION, false)
        val at = sbn.postTime
        val lines = linesOf(extras, title, at).ifEmpty {
            val text = (extras.getCharSequence(Notification.EXTRA_BIG_TEXT) ?: extras.getCharSequence(Notification.EXTRA_TEXT))?.toString()?.trim().orEmpty()
            if (text.isEmpty()) emptyList() else listOf(ChatLine(title, text, at))
        }
        if (title.isEmpty() || lines.isEmpty()) return

        val reply = n.actions?.firstOrNull { a -> a.remoteInputs?.isNotEmpty() == true && (a.semanticAction == Notification.Action.SEMANTIC_ACTION_REPLY || a.semanticAction == Notification.Action.SEMANTIC_ACTION_NONE) }
        Inbox.remember(sbn.key, reply)
        if (!group && app.replyWatch.matches(title)) {
            val text = lines.last().text
            app.net.execute { try { app.api.reportReply(title, text) } catch (_: Exception) {} }
        }
        Inbox.book.put(Chat(sbn.key, sbn.packageName, appLabel(sbn.packageName), title, group, lines, reply != null), System.currentTimeMillis())
    }

    /** The lines of a messaging-style notification, oldest first. */
    @Suppress("DEPRECATION")
    private fun linesOf(extras: Bundle, title: String, fallbackAt: Long): List<ChatLine> {
        val raw = extras.getParcelableArray(Notification.EXTRA_MESSAGES) ?: return emptyList()
        return raw.mapNotNull { it as? Bundle }.mapNotNull { b ->
            val text = b.getCharSequence("text")?.toString()?.trim().orEmpty()
            if (text.isEmpty()) null else {
                val sender = (b.getCharSequence("sender") ?: (b.getParcelable<android.app.Person>("sender_person"))?.name)?.toString()?.takeIf { it.isNotBlank() } ?: title
                ChatLine(sender, text, b.getLong("time", fallbackAt))
            }
        }
    }

    private fun appLabel(pkg: String): String = MESSAGING_APPS[pkg] ?: runCatching { packageManager.getApplicationLabel(packageManager.getApplicationInfo(pkg, 0)).toString() }.getOrDefault(pkg)

    companion object {
        /** Chat apps recognised by name, so they are read even when a notification is not marked as a message. */
        val MESSAGING_APPS = mapOf(
            "com.whatsapp" to "WhatsApp", "com.whatsapp.w4b" to "WhatsApp Business", "com.instagram.android" to "Instagram", "com.facebook.orca" to "Messenger",
            "org.telegram.messenger" to "Telegram", "org.thoughtcrime.securesms" to "Signal", "com.google.android.apps.messaging" to "Messages",
            "com.samsung.android.messaging" to "Messages", "com.oneplus.mms" to "Messages", "com.android.mms" to "Messages", "com.snapchat.android" to "Snapchat", "com.discord" to "Discord",
        )
    }
}
