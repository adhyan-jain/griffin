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
import com.griffin.assistant.core.Command
import com.griffin.assistant.core.Commands
import com.griffin.assistant.core.WakeMatcher

/**
 * The assistant. It listens in the background, wakes on the manager's name, does what the phone can do by itself
 * (calls, timers, texts, the flashlight), answers conversation through Griffin's fast voice route, and hands real
 * work to the manager. Everything it hears stays on the phone except the words sent to Griffin for an answer.
 */
class AssistantService : Service() {
    private val app get() = application as GriffinApp
    private lateinit var listener: Listener
    private lateinit var actions: Actions
    private var name = "JJ"
    private var awakeUntil = 0L
    private val history = ArrayList<Pair<String, String>>()
    private data class PendingText(val who: String, val number: String, val message: String, val expires: Long)
    private var pendingText: PendingText? = null
    private var debugReceiver: BroadcastReceiver? = null

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onCreate() {
        super.onCreate()
        actions = Actions(this)
        val nm = getSystemService(NotificationManager::class.java)
        nm.createNotificationChannel(NotificationChannel("assistant", "Listening", NotificationManager.IMPORTANCE_LOW))
        val stop = PendingIntent.getService(this, 1, Intent(this, AssistantService::class.java).setAction(ACTION_STOP), PendingIntent.FLAG_IMMUTABLE)
        val open = PendingIntent.getActivity(this, 0, Intent(this, MainActivity::class.java), PendingIntent.FLAG_IMMUTABLE)
        val notification: Notification = NotificationCompat.Builder(this, "assistant").setSmallIcon(android.R.drawable.ic_btn_speak_now).setContentTitle("Griffin is listening")
            .setContentText("Say the manager's name to start. Tap Stop to pause.").setOngoing(true).setContentIntent(open).addAction(0, "Stop", stop).build()
        startForeground(1, notification, ServiceInfo.FOREGROUND_SERVICE_TYPE_MICROPHONE)

        app.assistantRunning = true
        app.speaker.onSpeaking = { speaking -> if (::listener.isInitialized) { if (speaking) listener.pause() else listener.resume() } }
        listener = Listener(this, ::onHeard) { status -> app.log("mic: $status") }
        listener.start()
        refreshName()
        if (BuildConfig.DEBUG) registerDebugReceiver()
        app.log("assistant started")
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        if (intent?.action == ACTION_STOP) {
            stopSelf()
            return START_NOT_STICKY
        }
        return START_STICKY
    }

    override fun onDestroy() {
        app.assistantRunning = false
        app.speaker.onSpeaking = null
        app.speaker.stop()
        if (::listener.isInitialized) listener.stop()
        debugReceiver?.let { unregisterReceiver(it) }
        app.log("assistant stopped")
        super.onDestroy()
    }

    private fun refreshName() {
        app.net.execute {
            try {
                name = app.api.managerName()
                app.log("manager name: $name")
            } catch (e: Exception) {
                app.log("could not read the manager name (${e.message}); using $name")
            }
        }
    }

    /** Debug builds only: lets a test feed a sentence in as if it had been heard (adb shell am broadcast). */
    private fun registerDebugReceiver() {
        debugReceiver = object : BroadcastReceiver() {
            override fun onReceive(context: Context, intent: Intent) {
                intent.getStringExtra("text")?.let { app.log("debug input: $it"); onHeard(it) }
            }
        }
        ContextCompat.registerReceiver(this, debugReceiver, IntentFilter(ACTION_DEBUG_UTTERANCE), ContextCompat.RECEIVER_EXPORTED)
    }

    private fun say(text: String) {
        app.log("said: $text")
        app.speaker.say(text)
    }

    private fun onHeard(heard: String) {
        app.log("heard: $heard")
        val now = System.currentTimeMillis()

        // A text waiting for a yes.
        pendingText?.let { p ->
            pendingText = null
            if (now < p.expires) {
                when (heard.lowercase().trim(' ', '.', '!')) {
                    "yes", "send", "send it", "confirm", "yes send it", "yep", "yeah" -> { say(actions.sendSms(p.number, p.message) ?: "Sent to ${p.who}."); return }
                    "no", "cancel", "don't", "do not send", "nope", "stop" -> { say("Okay, not sending it."); return }
                }
            }
        }

        // A call that is ringing: "answer" and "reject" mean the call, with no need for the name.
        if (now < app.callWindowUntil) {
            when (Commands.parse(heard)) {
                Command.Answer -> { app.callWindowUntil = 0; actions.answer()?.let { say(it) }; return }
                Command.Reject -> { app.callWindowUntil = 0; actions.reject()?.let { say(it) }; return }
                else -> {}
            }
        }

        val wake = WakeMatcher.match(heard, name)
        val request = when {
            wake != null && wake.rest.isEmpty() -> { awakeUntil = now + 8_000; say("Yes?"); return }
            wake != null -> wake.rest
            now < awakeUntil -> heard
            else -> return // not for the assistant
        }
        awakeUntil = 0
        handle(request)
    }

    private fun handle(request: String) {
        when (val cmd = Commands.parse(request)) {
            is Command.Call -> placeCall(cmd.who)
            is Command.Text -> prepareText(cmd.who, cmd.message)
            is Command.Timer -> say(actions.timer(cmd.seconds))
            is Command.Alarm -> say(actions.alarm(cmd.hour, cmd.minute))
            is Command.Flashlight -> say(actions.flashlight(cmd.on))
            is Command.Volume -> say(actions.volume(cmd.up))
            is Command.OpenApp -> say(actions.openApp(cmd.name))
            Command.Stop -> app.speaker.stop()
            Command.Answer, Command.Reject -> say("There is no call ringing.")
            Command.None -> askGriffin(request)
        }
    }

    private fun hasPermission(p: String) = ContextCompat.checkSelfPermission(this, p) == PackageManager.PERMISSION_GRANTED

    private fun pick(who: String): Contacts.Match? {
        if (!hasPermission(Manifest.permission.READ_CONTACTS)) { say("I need permission to read your contacts."); return null }
        val found = Contacts.find(this, who)
        return when {
            found.isEmpty() -> { say("I could not find $who in your contacts."); null }
            found.size > 1 && !found[0].name.equals(who, true) -> { say("I found ${found.take(3).joinToString(", ") { it.name }}. Say the full name."); null }
            else -> found[0]
        }
    }

    private fun placeCall(who: String) {
        val m = pick(who) ?: return
        say("Calling ${m.name}.")
        actions.call(m.number)?.let { say(it) }
    }

    private fun prepareText(who: String, message: String) {
        val m = pick(who) ?: return
        pendingText = PendingText(m.name, m.number, message, System.currentTimeMillis() + 20_000)
        awakeUntil = 0
        say("Text ${m.name}: $message. Say send to confirm.")
        // The next thing heard is taken as the answer even without the name.
    }

    private fun askGriffin(request: String) {
        if (!app.api.signedIn) { say("Please sign in in the Griffin app first."); return }
        val past = history.takeLast(8).toList()
        app.net.execute {
            try {
                val r = app.api.voiceTurn(request, past)
                name = r.managerName
                app.main.post {
                    if (r.reply.isNotBlank()) say(r.reply)
                    history += "user" to request
                    if (r.reply.isNotBlank()) history += "assistant" to r.reply
                }
                if (r.handoff) {
                    app.net.execute {
                        try {
                            app.api.sendMessage(request, past + listOfNotNull("user" to request, if (r.reply.isNotBlank()) "assistant" to r.reply else null))
                            app.log("handed to ${r.managerName}: $request")
                            if (r.reply.isBlank()) app.main.post { say("One moment.") }
                        } catch (e: Exception) {
                            app.main.post { say("I could not reach ${r.managerName}. ${e.message}") }
                        }
                    }
                }
            } catch (e: ApiException) {
                app.main.post { say(if (e.status == 401) "Your sign-in has expired. Please sign in again." else "I could not reach Griffin. ${e.message}") }
            } catch (e: Exception) {
                app.main.post { say("I could not reach Griffin. Check the connection.") }
            }
        }
    }

    companion object {
        const val ACTION_STOP = "com.griffin.assistant.STOP"
        const val ACTION_DEBUG_UTTERANCE = "com.griffin.assistant.DEBUG_UTTERANCE"
    }
}
