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
import android.net.Uri
import android.os.IBinder
import android.os.PowerManager
import androidx.core.app.NotificationCompat
import androidx.core.content.ContextCompat
import com.griffin.assistant.core.AEvent
import com.griffin.assistant.core.AState
import com.griffin.assistant.core.Aliases
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
        try { nm.deleteNotificationChannel("assistant") } catch (_: Exception) {}
        val channel = NotificationChannel("assistant_silent", "Griffin Background", NotificationManager.IMPORTANCE_MIN).apply {
            setShowBadge(false)
            enableLights(false)
            enableVibration(false)
            setSound(null, null)
            lockscreenVisibility = Notification.VISIBILITY_SECRET
        }
        nm.createNotificationChannel(channel)
        val updateChannel = NotificationChannel("griffin_task_updates", "Griffin Tasks & Reports", NotificationManager.IMPORTANCE_HIGH).apply {
            description = "Notifications for completed tasks, daily/weekly briefings, and DSA updates"
            enableVibration(true)
            setShowBadge(true)
            lockscreenVisibility = Notification.VISIBILITY_PUBLIC
        }
        nm.createNotificationChannel(updateChannel)
        val open = PendingIntent.getActivity(this, 0, Intent(this, MainActivity::class.java), PendingIntent.FLAG_IMMUTABLE)
        val notification: Notification = NotificationCompat.Builder(this, "assistant_silent")
            .setSmallIcon(R.drawable.ic_stat_griffin)
            .setContentTitle("Griffin")
            .setContentText("Background service active")
            .setOngoing(true)
            .setContentIntent(open)
            .setPriority(NotificationCompat.PRIORITY_MIN)
            .setVisibility(NotificationCompat.VISIBILITY_SECRET)
            .setSilent(true)
            .setShowWhen(false)
            .build()
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
        if (ListenMode.wakeWord(this)) listener.start() else app.log("tap-only: the microphone stays off until you tap")
        refreshName()
        app.net.execute { try { if (app.api.signedIn) app.api.refreshAliases() } catch (_: Exception) {} }
        app.main.post(deviceBeat)
        if (BuildConfig.DEBUG) registerDebugReceiver()
        app.log("assistant started")
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        when (intent?.action) {
            ACTION_STOP -> { stopSelf(); return START_NOT_STICKY }
            // From the button, the notification or the tile: the same as hearing "hey <name>" on its own.
            ACTION_LISTEN -> {
                app.log("woke by button")
                if (::listener.isInitialized) { if (ListenMode.wakeWord(this)) listener.forceListen() else listener.listenOnce() }
                dispatch(AEvent.ManualWake(System.currentTimeMillis()))
                beepSoon()
            }
            // The owner changed how it wakes: turn the always-on microphone on or off to match.
            ACTION_APPLY_MODE -> if (::listener.isInitialized) { if (ListenMode.wakeWord(this)) listener.start() else listener.stop() }
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
        app.main.removeCallbacks(deviceBeat)
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
            PendingAction.Kind.Call -> {
                say("Calling ${a.who}.")
                actions.call(a.target)?.let { say(it) }
            }
            PendingAction.Kind.AutomatedCall -> {
                executeAutomatedCall(a.target, a.who, a.text, inBackground = a.via == "background", listenOnly = false)
            }
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

    /** Tells Griffin this phone is alive every 30 seconds, so it shows as a live device (it counts as live for 45). */
    private var beats = 0
    private val phoneRunner by lazy { PhoneRunner(this, app.api, app.cellularCallManager) { app.dlog(it) } }
    private val deviceBeat: Runnable = object : Runnable {
        override fun run() {
            val refreshNames = beats++ % 10 == 0
            app.beat.execute {
                try { app.api.phoneHeartbeat(withPhoneUi = GriffinAccess.ready) } catch (e: Exception) { app.log("device heartbeat failed: ${e.message}") }
            }
            app.jobs.execute { try { phoneRunner.tick() } catch (e: Exception) { app.dlog("phone job: ${e.message}") } }
            if (refreshNames) app.net.execute { try { app.replyWatch.update(app.api.awaitingNames()) } catch (_: Exception) {} }
            checkAndNotifyCompletedTasks()
            app.main.postDelayed(this, 30_000)
        }
    }

    private fun checkAndNotifyCompletedTasks() {
        if (!app.api.signedIn) return
        app.net.execute {
            try {
                val tasks = app.api.fetchRecentCompletedTasks()
                if (tasks.isEmpty()) return@execute
                val prefs = getSharedPreferences("griffin_task_notifs", Context.MODE_PRIVATE)
                val seen = prefs.getStringSet("seen_tasks", emptySet())?.toMutableSet() ?: mutableSetOf()
                val newlyCompleted = tasks.filter { it.id !in seen }
                if (newlyCompleted.isEmpty()) return@execute

                val nm = getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
                for (task in newlyCompleted) {
                    seen.add(task.id)
                    val resp = task.response.trim()
                    if (resp.isEmpty() ||
                        resp.contains("cannot inspect", ignoreCase = true) ||
                        resp.contains("cannot access", ignoreCase = true) ||
                        resp.contains("couldn't find any information", ignoreCase = true) ||
                        resp.contains("I have NO tools", ignoreCase = true) ||
                        resp.contains("do not have tool", ignoreCase = true)
                    ) {
                        continue
                    }
                    val title = when {
                        task.title.contains("DSA", ignoreCase = true) || task.title.contains("LeetCode", ignoreCase = true) ->
                            "DSA / LeetCode Update"
                        task.title.contains("Brief", ignoreCase = true) || task.title.contains("Morning", ignoreCase = true) ->
                            "Daily Executive Briefing Ready"
                        task.title.contains("Post", ignoreCase = true) || task.title.contains("Weekly", ignoreCase = true) ->
                            "Weekly Report Ready"
                        else -> "Task Completed: ${task.title.take(50)}"
                    }
                    val body = resp.take(250)
                    val appIntent = Intent(this, MainActivity::class.java).apply {
                        flags = Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_SINGLE_TOP or Intent.FLAG_ACTIVITY_CLEAR_TOP
                        putExtra("task_id", task.id)
                        putExtra("task_title", task.title)
                        putExtra("task_response", resp)
                    }
                    val pi = PendingIntent.getActivity(this, task.id.hashCode(), appIntent, PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE)

                    // Wake up screen if locked/asleep so notification is immediately noticed
                    try {
                        val pm = getSystemService(Context.POWER_SERVICE) as PowerManager
                        @Suppress("DEPRECATION")
                        val wakeLock = pm.newWakeLock(
                            PowerManager.FULL_WAKE_LOCK or PowerManager.ACQUIRE_CAUSES_WAKEUP or PowerManager.ON_AFTER_RELEASE,
                            "griffin:task_completion_alert"
                        )
                        wakeLock.acquire(4000L)
                    } catch (e: Exception) {
                        app.dlog("wake lock acquire failed: ${e.message}")
                    }

                    val notif = NotificationCompat.Builder(this, "griffin_task_updates")
                        .setSmallIcon(R.drawable.ic_stat_griffin)
                        .setContentTitle(title)
                        .setContentText(body)
                        .setStyle(NotificationCompat.BigTextStyle().bigText(task.response.ifEmpty { task.goal }))
                        .setContentIntent(pi)
                        .setAutoCancel(true)
                        .setPriority(NotificationCompat.PRIORITY_MAX)
                        .setVisibility(NotificationCompat.VISIBILITY_PUBLIC)
                        .setCategory(NotificationCompat.CATEGORY_EVENT)
                        .setDefaults(Notification.DEFAULT_ALL)
                        .build()

                    nm.notify(task.id.hashCode(), notif)
                }
                prefs.edit().putStringSet("seen_tasks", seen.toList().takeLast(100).toSet()).apply()
            } catch (e: Exception) {
                app.dlog("task notif check failed: ${e.message}")
            }
        }
    }

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

    private fun handle(heard: String) {
        // The owner's aliases ("mummy" means "mom") are understood by what the phone does itself. Anything it does not
        // recognise goes to Griffin as said, and the server applies the aliases again.
        val request = heard
        val cmd0 = Commands.parse(Aliases.apply(heard, app.api.aliases()))
        when (val cmd = cmd0) {
            is Command.Call -> {
                if (cmd.channel == "whatsapp") {
                    say(actions.callWhatsApp(cmd.who) ?: "Calling ${cmd.who} on WhatsApp.")
                } else {
                    placeCall(cmd.who, cmd.inBackground)
                }
            }
            is Command.AutomatedCall -> {
                if (cmd.channel == "whatsapp") {
                    say(actions.callWhatsApp(cmd.who) ?: "Calling ${cmd.who} on WhatsApp.")
                } else {
                    makeAutomatedCall(cmd.who, cmd.message, cmd.inBackground, cmd.listenOnly)
                }
            }
            is Command.CancelCall -> cancelActiveCall(cmd.who)
            is Command.Text -> textOrReply(cmd.who, cmd.message, cmd.inBackground)
            is Command.Timer -> say(actions.timer(cmd.seconds))
            is Command.Alarm -> say(actions.alarm(cmd.hour, cmd.minute))
            is Command.Flashlight -> say(actions.flashlight(cmd.on))
            is Command.Volume -> say(actions.volume(cmd.up))
            is Command.Mute -> say(actions.mute(cmd.mute))
            Command.Battery -> say(actions.battery())
            Command.Time -> say(actions.time())
            Command.Date -> say(actions.date())
            Command.Camera -> say(actions.openCamera())
            Command.MediaNext -> say(actions.mediaNext())
            Command.MediaPrevious -> say(actions.mediaPrevious())
            Command.MediaPlay -> say(actions.mediaPlay())
            Command.MediaPause -> say(actions.mediaPause())
            Command.MediaPlayPause -> say(actions.mediaPlayPause())
            is Command.OpenApp -> say(actions.openApp(cmd.name))
            is Command.OpenChat -> openChat(cmd.who, cmd.app)
            Command.Ringtone -> say(actions.openRingtoneSettings())
            Command.Wallpaper -> say(actions.openWallpaperSettings())
            Command.DeveloperOptions -> say(actions.openDeveloperOptions())
            is Command.WebSearch -> say(actions.searchWeb(cmd.query))
            is Command.ReadMessages -> readMessages(cmd.from)
            is Command.Reply -> replyTo(cmd.who, cmd.app, cmd.text, cmd.inBackground)
            is Command.MessageVia -> messageVia(cmd.who, cmd.app, cmd.text, cmd.inBackground)
            Command.Stop -> { app.speaker.stop(); finished() }
            Command.Answer, Command.Reject -> say("There is no call ringing.")
            Command.None -> askGriffin(request)
        }
    }

    private fun cancelActiveCall(who: String?) {
        if (!app.cellularCallManager.isCallActive) {
            say("There is no active call to cancel.")
            return
        }
        say("Cancelling call.")
        app.cellularCallManager.cancelCurrentCall(who) { result ->
            app.main.post {
                say(result)
            }
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

    private fun placeCall(who: String, inBackground: Boolean = false) {
        val m = pickContact(who) ?: return
        confirm(
            PendingAction(PendingAction.Kind.Call, m.name, m.number, "", if (inBackground) "background" else "direct"),
            "Call ${m.name}? Say yes to confirm."
        )
    }

    private fun makeAutomatedCall(who: String, message: String, inBackground: Boolean = false, listenOnly: Boolean = false) {
        val isDigits = who.matches(Regex("^[+0-9 -]{7,}$"))
        val number: String
        val displayName: String
        if (isDigits) {
            number = who
            displayName = who
        } else {
            val m = pickContact(who) ?: return
            number = m.number
            displayName = m.name
        }
        confirm(
            PendingAction(PendingAction.Kind.AutomatedCall, displayName, number, message, if (inBackground) "background" else "supervised"),
            "Automated call to $displayName to say: $message. Say yes to confirm."
        )
    }

    private fun executeAutomatedCall(number: String, displayName: String, message: String, inBackground: Boolean, listenOnly: Boolean) {
        if (inBackground) {
            say("Placing automated call to $displayName in background.")
        } else if (listenOnly) {
            say("Calling $displayName on speaker.")
        } else {
            say("Placing automated call to $displayName.")
        }
        if (::listener.isInitialized) listener.pause()
        app.jobs.execute {
            try {
                app.cellularCallManager.startAutomatedCall(
                    number = number,
                    recipientName = displayName,
                    message = message,
                    inBackground = inBackground,
                    onDone = { _, _, _ ->
                        app.main.post {
                            if (::listener.isInitialized && ListenMode.wakeWord(this)) {
                                listener.resume()
                            }
                        }
                    }
                )
            } catch (e: Exception) {
                app.log("automated call failed: ${e.message}")
                app.main.post {
                    if (::listener.isInitialized && ListenMode.wakeWord(this)) {
                        listener.resume()
                    }
                }
            }
        }
    }

    private fun textOrReply(who: String, message: String, inBackground: Boolean = false) {
        // If that person has just messaged in a chat app, a text means "answer them there"; otherwise a plain text message.
        val chats = Inbox.book.replyTarget(who, null, System.currentTimeMillis())
        if (chats.size == 1) {
            if (inBackground) {
                val err = Inbox.reply(this, chats[0].key, message)
                if (err == null) say("Replied to ${chats[0].title} on ${chats[0].appName} in background.") else say(err)
            } else {
                confirmReply(chats[0], message)
            }
            return
        }
        val m = pickContact(who) ?: return
        if (inBackground) {
            val err = actions.sendSms(m.number, message)
            if (err == null) say("Sent text to ${m.name} in background.") else say(err)
            return
        }
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

    /** Opens one chat (WhatsApp only for now) from the contact's number. Nothing is typed or sent. */
    private fun openChat(who: String, appName: String) {
        if (appName != "WhatsApp") { say(actions.openApp(appName) + " I can only jump straight into a chat on WhatsApp for now."); return }
        val m = pickContact(who) ?: return
        say(actions.openWhatsAppChat(m.number, m.name))
    }

    private fun replyTo(who: String?, appSaid: String?, text: String, inBackground: Boolean = false) {
        if (!notificationAccess()) { say("I need access to your notifications to reply. Turn it on in the Griffin app."); return }
        val now = System.currentTimeMillis()
        val justRead = if (who == null) lastRead?.let { k -> Inbox.book.all(now).firstOrNull { it.key == k && it.canReply } } else null
        val candidates = if (justRead != null) listOf(justRead) else Inbox.book.replyTarget(who, appSaid, now)
        when {
            candidates.isEmpty() -> say(if (who == null) "There is no chat to reply to. Say who, for example: reply to Bro saying I am on my way." else "I have no recent chat with $who that I can reply to.")
            candidates.size > 1 && who != null && candidates.map { it.appName }.distinct().size > 1 -> say("I found ${candidates.joinToString(" and ") { "${it.title} on ${it.appName}" }}. Say which app.")
            else -> {
                if (inBackground) {
                    val err = Inbox.reply(this, candidates[0].key, text)
                    if (err == null) say("Replied to ${candidates[0].title} on ${candidates[0].appName} in background.") else say(err)
                } else {
                    confirmReply(candidates[0], text)
                }
            }
        }
    }

    private fun messageVia(who: String, appName: String, text: String, inBackground: Boolean = false) {
        val chats = if (notificationAccess()) Inbox.book.replyTarget(who, appName, System.currentTimeMillis()) else emptyList()
        when {
            chats.size == 1 -> {
                if (inBackground) {
                    val err = Inbox.reply(this, chats[0].key, text)
                    if (err == null) say("Sent message to ${chats[0].title} on ${chats[0].appName} in background.") else say(err)
                } else {
                    confirmReply(chats[0], text)
                }
            }
            appName == "Messages" -> textOrReply(who, text, inBackground)
            appName == "WhatsApp" && app.api.signedIn -> app.net.execute {
                // No open chat to reply to: let Griffin send it from the laptop's WhatsApp Web in background.
                try {
                    app.api.sendMessage("Send a WhatsApp message to $who: $text", emptyList())
                    app.main.post { say("Asked Griffin to send WhatsApp to $who in background from your laptop.") }
                } catch (e: Exception) {
                    app.main.post { say("I could not reach Griffin to send that.") }
                }
            }
            else -> {
                if (inBackground) {
                    val m = pickContact(who)
                    if (m != null) {
                        val err = actions.sendSms(m.number, text)
                        if (err == null) say("Sent text to ${m.name} in background.") else say(err)
                    } else {
                        say("I can reply to recent chats in background, but cannot start a new chat in $appName yet.")
                    }
                } else {
                    say("I can reply to $appName chats that have just messaged you, but I cannot start a new one in $appName yet.")
                }
            }
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
                if (request.contains("alias", ignoreCase = true)) try { app.api.refreshAliases() } catch (_: Exception) {}
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
        const val ACTION_APPLY_MODE = "com.griffin.assistant.APPLY_MODE"
        const val ACTION_STOP = "com.griffin.assistant.STOP"
        const val ACTION_LISTEN = "com.griffin.assistant.LISTEN"
        const val ACTION_DEBUG_UTTERANCE = "com.griffin.assistant.DEBUG_UTTERANCE"
    }
}
