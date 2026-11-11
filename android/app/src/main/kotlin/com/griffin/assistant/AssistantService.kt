package com.griffin.assistant

import android.Manifest
import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.content.pm.PackageManager
import android.content.pm.ServiceInfo
import android.os.IBinder
import androidx.core.app.NotificationCompat
import androidx.core.content.ContextCompat
import com.griffin.assistant.core.AEvent
import com.griffin.assistant.core.AState
import com.griffin.assistant.core.AssistantMachine
import com.griffin.assistant.core.Chat
import com.griffin.assistant.core.Command
import com.griffin.assistant.core.Commands
import com.griffin.assistant.core.Effect
import com.griffin.assistant.core.MessageBook
import com.griffin.assistant.core.PendingAction
import com.griffin.assistant.core.Phase

/**
 * The assistant. It listens in the background, wakes on "hey" and the manager's name, does what the phone can do by
 * itself, reads out and replies to chats through the chat apps' own notifications (so nothing opens on screen and a
 * game keeps running), answers conversation through Griffin, and hands real work to the manager.
 *
 * Every decision about *what state it is in* is made by [AssistantMachine]; this class only feeds it events and
 * carries out the effects it returns. All of it runs on the main thread.
 */
class AssistantService : Service() {
    private val app get() = application as GriffinApp
    private lateinit var listener: Listener
    private lateinit var actions: Actions
    private lateinit var overlay: ListeningOverlay
    private var name = "JJ"
    private var state = AState()
    private val history = ArrayList<Pair<String, String>>()
    private var lastRead: String? = null
    private var debugReceiver: BroadcastReceiver? = null

    private val timer = Runnable { dispatch(AEvent.Tick(System.currentTimeMillis())) }
    private val watchdog = Runnable { dispatch(if (state.phase == Phase.Speaking) AEvent.SpeechEnded(System.currentTimeMillis()) else AEvent.WorkFinished) }
    private val orbListener: () -> Unit = { if (app.mode == OrbView.Mode.Hidden) overlay.hide() else { overlay.show(); overlay.update(app.mode, app.level) } }

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onCreate() {
        super.onCreate()
        actions = Actions(this)
        val nm = getSystemService(NotificationManager::class.java)
        nm.createNotificationChannel(NotificationChannel("assistant", "Listening", NotificationManager.IMPORTANCE_MIN))
        val stop = PendingIntent.getService(this, 1, Intent(this, AssistantService::class.java).setAction(ACTION_STOP), PendingIntent.FLAG_IMMUTABLE)
        val listenNow = PendingIntent.getService(this, 2, Intent(this, AssistantService::class.java).setAction(ACTION_LISTEN), PendingIntent.FLAG_IMMUTABLE)
        val open = PendingIntent.getActivity(this, 0, Intent(this, MainActivity::class.java), PendingIntent.FLAG_IMMUTABLE)
        val notification: Notification = NotificationCompat.Builder(this, "assistant").setSmallIcon(android.R.drawable.ic_btn_speak_now).setContentTitle("Griffin is listening")
            .setContentText("Say \"hey $name\" or \"oye bhosadike\", or tap Listen now.").setOngoing(true).setContentIntent(open).addAction(0, "Listen now", listenNow).addAction(0, "Stop", stop).build()
        startForeground(1, notification, ServiceInfo.FOREGROUND_SERVICE_TYPE_MICROPHONE)

        app.assistantRunning = true
        overlay = ListeningOverlay(this)
        app.addModeListener(orbListener)
        app.assistantEvents = { e -> dispatch(e) }
        app.speaker.onSpeaking = { speaking ->
            if (::listener.isInitialized) { if (speaking) listener.pause() else listener.resume() }
            dispatch(if (speaking) AEvent.SpeechStarted else AEvent.SpeechEnded(System.currentTimeMillis()))
        }
        listener = Listener(
            this,
            { candidates ->
                val primary = candidates.firstOrNull().orEmpty()
                app.dlog("mic heard: $candidates")
                dispatch(AEvent.Heard(primary, System.currentTimeMillis(), name, candidates))
            },
            { status -> app.log("mic: $status") },
            { level -> if (app.mode == OrbView.Mode.Listening) app.showLevel(level) }
        )
        listener.start()
        refreshName()
        if (BuildConfig.DEBUG) registerDebugReceiver()
        app.log("assistant started")
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        when (intent?.action) {
            ACTION_STOP -> { stopSelf(); return START_NOT_STICKY }
            // From the button, the notification or the tile: the same as hearing "hey <name>" on its own.
            ACTION_LISTEN -> { app.log("woke by button"); dispatch(AEvent.ManualWake(System.currentTimeMillis())); beepSoon() }
        }
        return START_STICKY
    }

    override fun onDestroy() {
        app.assistantRunning = false
        app.assistantEvents = null
        app.speaker.onSpeaking = null
        app.speaker.stop()
        app.main.removeCallbacks(timer)
        app.main.removeCallbacks(watchdog)
        if (::listener.isInitialized) listener.stop()
        debugReceiver?.let { unregisterReceiver(it) }
        app.removeModeListener(orbListener)
        app.showMode(OrbView.Mode.Hidden)
        if (::overlay.isInitialized) overlay.hide()
        app.log("assistant stopped")
        super.onDestroy()
    }

    // ---- the state machine ------------------------------------------------------------------------------------

    private fun dispatch(e: AEvent) {
        val step = AssistantMachine.reduce(state, e)
        val before = state.phase
        state = step.state
        if (before != state.phase) {
            app.showMode(when (AssistantMachine.orb(state.phase)) { "listening" -> OrbView.Mode.Listening; "thinking" -> OrbView.Mode.Thinking; "speaking" -> OrbView.Mode.Speaking; else -> OrbView.Mode.Hidden })
            if (state.phase == Phase.Awake || state.phase == Phase.Confirming) app.showLevel(0f)
        }
        step.effects.forEach(::perform)
        app.main.removeCallbacks(timer)
        app.main.removeCallbacks(watchdog)
        val now = System.currentTimeMillis()
        when (state.phase) {
            Phase.Awake, Phase.Confirming -> app.main.postDelayed(timer, maxOf(0L, state.until - now) + 20)
            Phase.Thinking -> app.main.postDelayed(watchdog, 30_000)   // never stay "thinking" forever
            Phase.Speaking -> app.main.postDelayed(watchdog, 45_000)   // nor "speaking"
            Phase.Idle -> {}
        }
    }

    private fun perform(effect: Effect) {
        when (effect) {
            Effect.Ignore -> {}
            Effect.Beep -> { app.log("woke"); beep() }
            is Effect.Handle -> handle(effect.request)
            is Effect.Confirmed -> carryOut(effect.action)
            is Effect.Declined -> say(if (effect.action.kind == PendingAction.Kind.Server) "Okay, I will leave it." else "Okay, not sending it.")
            is Effect.ReadDraft -> {
                val draft = effect.action.text.ifBlank { effect.action.who.ifBlank { "No draft text attached." } }
                say("The draft says: $draft. Say approve to confirm, or say leave it.")
            }
            is Effect.Hold -> say("Okay, I will hold it for you to check.")
            Effect.AnswerCall -> actions.answer()?.let { say(it) }
            Effect.RejectCall -> actions.reject()?.let { say(it) }
        }
    }

    private fun carryOut(a: PendingAction) {
        when (a.kind) {
            PendingAction.Kind.Sms -> say(actions.sendSms(a.target, a.text) ?: "Sent to ${a.who}.")
            PendingAction.Kind.Reply -> say(Inbox.reply(this, a.target, a.text) ?: "Replied to ${a.who} on ${a.via}.")
            PendingAction.Kind.Server -> app.net.execute {
                try {
                    val r = app.api.voiceConfirm(a.target, true, unlocked())
                    app.main.post { say(r.reply.ifBlank { "Done." }) }
                } catch (e: Exception) {
                    app.main.post { say("I could not reach Griffin to do that.") }
                }
            }
        }
    }

    // ---- helpers ----------------------------------------------------------------------------------------------

    private fun refreshName() {
        app.net.execute {
            try {
                name = app.api.managerName()
                app.log("manager name loaded")
            } catch (e: Exception) {
                app.log("could not read the manager name; using the last one")
            }
        }
    }

    /** Debug builds only: lets a test feed a sentence in as if it had been heard (adb shell am broadcast). */
    private fun registerDebugReceiver() {
        debugReceiver = object : BroadcastReceiver() {
            override fun onReceive(context: Context, intent: Intent) {
                intent.getStringExtra("text")?.let { dispatch(AEvent.Heard(it, System.currentTimeMillis(), name)) }
            }
        }
        ContextCompat.registerReceiver(this, debugReceiver, IntentFilter(ACTION_DEBUG_UTTERANCE), ContextCompat.RECEIVER_EXPORTED)
    }

    private fun say(text: String) {
        app.dlog("said: $text")
        app.speaker.say(text)
    }

    private fun finished() = dispatch(AEvent.WorkFinished)

    /** Changes to tasks and approvals by voice need the phone unlocked: a spoken yes does not prove who said it. */
    private fun unlocked() = !(getSystemService(android.app.KeyguardManager::class.java)?.isDeviceLocked ?: true)

    private fun beepSoon() = beep()

    private fun beep() {
        try {
            android.media.ToneGenerator(android.media.AudioManager.STREAM_MUSIC, 60).startTone(android.media.ToneGenerator.TONE_PROP_ACK, 140)
        } catch (_: Exception) {}
    }

    private fun hasPermission(p: String) = ContextCompat.checkSelfPermission(this, p) == PackageManager.PERMISSION_GRANTED

    private fun confirm(action: PendingAction, spoken: String) {
        dispatch(AEvent.ConfirmRequested(action, System.currentTimeMillis()))
        say(spoken)
    }

    // ---- what the owner asked ---------------------------------------------------------------------------------

    private fun handle(request: String) {
        when (val cmd = Commands.parse(request)) {
            is Command.Call -> placeCall(cmd.who)
            is Command.Text -> textOrReply(cmd.who, cmd.message)
            is Command.Timer -> say(actions.timer(cmd.seconds))
            is Command.Alarm -> say(actions.alarm(cmd.hour, cmd.minute))
            is Command.Flashlight -> say(actions.flashlight(cmd.on))
            is Command.Volume -> say(actions.volume(cmd.up))
            Command.MediaNext -> say(actions.mediaNext())
            Command.MediaPrevious -> say(actions.mediaPrevious())
            Command.MediaPlay -> say(actions.mediaPlay())
            Command.MediaPause -> say(actions.mediaPause())
            Command.MediaPlayPause -> say(actions.mediaPlayPause())
            is Command.OpenApp -> say(actions.openApp(cmd.name))
            is Command.ReadMessages -> readMessages(cmd.from)
            is Command.Reply -> replyTo(cmd.who, cmd.app, cmd.text)
            is Command.MessageVia -> messageVia(cmd.who, cmd.app, cmd.text)
            Command.Stop -> { app.speaker.stop(); finished() }
            Command.Answer, Command.Reject -> say("There is no call ringing.")
            Command.None -> askGriffin(request)
        }
    }

    private fun pickContact(who: String): Contacts.Match? {
        if (!hasPermission(Manifest.permission.READ_CONTACTS)) { say("I need permission to read your contacts."); return null }
        val found = Contacts.find(this, who)
        return when {
            found.isEmpty() -> { say("I could not find $who in your contacts."); null }
            found.size > 1 && !found[0].name.equals(who, true) -> { say("I found ${found.take(3).joinToString(", ") { it.name }}. Say the full name."); null }
            else -> found[0]
        }
    }

    private fun placeCall(who: String) {
        val m = pickContact(who) ?: return
        say("Calling ${m.name}.")
        actions.call(m.number)?.let { say(it) }
    }

    private fun textOrReply(who: String, message: String) {
        // If that person has just messaged in a chat app, a text means "answer them there"; otherwise a plain text message.
        val chats = Inbox.book.replyTarget(who, null, System.currentTimeMillis())
        if (chats.size == 1) { confirmReply(chats[0], message); return }
        val m = pickContact(who) ?: return
        confirm(PendingAction(PendingAction.Kind.Sms, m.name, m.number, message, "text message"), "Text ${m.name}: $message. Say send to confirm.")
    }

    private fun confirmReply(chat: Chat, text: String) {
        confirm(PendingAction(PendingAction.Kind.Reply, chat.title, chat.key, text, chat.appName), "Reply to ${chat.title} on ${chat.appName}: $text. Say send to confirm.")
    }

    private fun readMessages(from: String?) {
        if (!notificationAccess()) { say("I need access to your notifications to read messages. Turn it on in the Griffin app."); return }
        val now = System.currentTimeMillis()
        val chats = if (from == null) Inbox.book.unread(now) else Inbox.book.from(from, now)
        if (chats.isEmpty()) { say(if (from == null) "You have no new messages." else "I have no recent messages from $from."); return }
        Inbox.book.markRead(chats.map { it.key })
        lastRead = chats.first().key
        say(MessageBook.spoken(chats))
    }

    private fun replyTo(who: String?, appSaid: String?, text: String) {
        if (!notificationAccess()) { say("I need access to your notifications to reply. Turn it on in the Griffin app."); return }
        val now = System.currentTimeMillis()
        val justRead = if (who == null) lastRead?.let { k -> Inbox.book.all(now).firstOrNull { it.key == k && it.canReply } } else null
        val candidates = if (justRead != null) listOf(justRead) else Inbox.book.replyTarget(who, appSaid, now)
        when {
            candidates.isEmpty() -> say(if (who == null) "There is no chat to reply to. Say who, for example: reply to Bro saying I am on my way." else "I have no recent chat with $who that I can reply to.")
            candidates.size > 1 && who != null && candidates.map { it.appName }.distinct().size > 1 -> say("I found ${candidates.joinToString(" and ") { "${it.title} on ${it.appName}" }}. Say which app.")
            else -> confirmReply(candidates[0], text)
        }
    }

    private fun messageVia(who: String, appName: String, text: String) {
        val chats = if (notificationAccess()) Inbox.book.replyTarget(who, appName, System.currentTimeMillis()) else emptyList()
        when {
            chats.size == 1 -> confirmReply(chats[0], text)
            appName == "Messages" -> textOrReply(who, text)
            appName == "WhatsApp" && app.api.signedIn -> app.net.execute {
                // No open chat to reply to: let Griffin send it from the laptop's WhatsApp Web, with the usual approval.
                try {
                    app.api.sendMessage("Send a WhatsApp message to $who: $text", emptyList())
                    app.main.post { say("There is no open WhatsApp chat with $who, so I asked Griffin to send it from your laptop. You will be asked to approve it.") }
                } catch (e: Exception) {
                    app.main.post { say("I could not reach Griffin to send that.") }
                }
            }
            else -> say("I can reply to $appName chats that have just messaged you, but I cannot start a new one in $appName yet.")
        }
    }

    private fun notificationAccess() = androidx.core.app.NotificationManagerCompat.getEnabledListenerPackages(this).contains(packageName)

    private fun askGriffin(request: String) {
        if (!app.api.signedIn) { say("Please sign in in the Griffin app first."); return }
        val past = history.takeLast(8).toList()
        val unlocked = unlocked()
        app.net.execute {
            try {
                val r = app.api.voiceTurn(request, past, unlocked)
                name = r.managerName
                app.main.post {
                    // Griffin asked for a yes before changing anything: wait for it with the animation showing.
                    r.confirmToken?.let { dispatch(AEvent.ConfirmRequested(PendingAction(PendingAction.Kind.Server, who = r.reply, target = it, text = r.draft.orEmpty(), via = ""), System.currentTimeMillis())) }
                    if (r.reply.isNotBlank()) say(r.reply) else if (!r.handoff) finished()
                    history += "user" to request
                    if (r.reply.isNotBlank()) history += "assistant" to r.reply
                }
                if (r.handoff) {
                    app.net.execute {
                        try {
                            app.api.sendMessage(request, past + listOfNotNull("user" to request, if (r.reply.isNotBlank()) "assistant" to r.reply else null))
                            app.log("handed to the manager")
                            if (r.reply.isBlank()) app.main.post { say("One moment.") }
                        } catch (e: Exception) {
                            app.main.post { say("I could not reach ${r.managerName}. ${e.message}") }
                        }
                    }
                }
            } catch (e: ApiException) {
                app.log("griffin: ${e.message}")
                app.main.post {
                    say(when {
                        e.status == 401 -> "Your sign-in has expired. Please sign in again."
                        e.status == 0 -> "I could not reach Griffin. Check the connection."
                        else -> "Griffin answered with an error. ${e.message}"
                    })
                }
            } catch (e: Exception) {
                app.main.post { say("I could not reach Griffin. Check the connection.") }
            }
        }
    }

    companion object {
        const val ACTION_STOP = "com.griffin.assistant.STOP"
        const val ACTION_LISTEN = "com.griffin.assistant.LISTEN"
        const val ACTION_DEBUG_UTTERANCE = "com.griffin.assistant.DEBUG_UTTERANCE"
    }
}
