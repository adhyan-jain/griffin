package com.griffin.assistant

import android.app.Application
import android.os.Handler
import android.os.Looper
import java.util.concurrent.Executors

/** Holds the few things that live as long as the app: sign-in, the voice, and a short log the screen shows. */
class GriffinApp : Application() {
    lateinit var store: SecretStore
    lateinit var api: ApiClient
    lateinit var speaker: Speaker
    val net = Executors.newSingleThreadExecutor()
    val main = Handler(Looper.getMainLooper())

    private val lines = ArrayDeque<String>()
    var onLog: (() -> Unit)? = null

    /** Set while a call is ringing and the assistant is listening, so "answer" and "reject" mean the call. */
    @Volatile var callWindowUntil = 0L
    @Volatile var assistantRunning = false

    override fun onCreate() {
        super.onCreate()
        store = SecretStore(this)
        api = ApiClient(store, getSharedPreferences("griffin", MODE_PRIVATE))
        speaker = Speaker(this)
        if (BuildConfig.DEBUG) registerDebugHooks()
    }

    /** Debug builds only: sign in and point the app at another API from adb, so tests need no typing. */
    private fun registerDebugHooks() {
        val receiver = object : android.content.BroadcastReceiver() {
            override fun onReceive(context: android.content.Context, intent: android.content.Intent) {
                intent.getStringExtra("api")?.let { api.apiUrl = it }
                intent.getStringExtra("auth")?.let { api.authUrl = it }
                val email = intent.getStringExtra("email") ?: return
                net.execute {
                    try { api.signIn(email, intent.getStringExtra("password") ?: ""); log("debug sign-in ok") } catch (e: Exception) { log("debug sign-in failed: ${e.message}") }
                }
            }
        }
        androidx.core.content.ContextCompat.registerReceiver(this, receiver, android.content.IntentFilter("com.griffin.assistant.DEBUG_SIGNIN"), androidx.core.content.ContextCompat.RECEIVER_EXPORTED)
    }

    fun log(line: String) {
        android.util.Log.i("Griffin", line)
        main.post {
            lines.addFirst(line)
            while (lines.size > 40) lines.removeLast()
            onLog?.invoke()
        }
    }

    fun logText(): String = lines.joinToString("\n")

    /** A call is ringing: say who it is. Called by the call-screening service. */
    fun announceIncoming(number: String?) {
        val name = number?.let { Contacts.nameForNumber(this, it) }
        val who = when {
            name != null -> name
            number.isNullOrBlank() -> "a private number"
            else -> "an unknown number"
        }
        val prompt = if (assistantRunning) "Call from $who. Say answer or reject." else "Call from $who."
        log("incoming call: $who")
        if (assistantRunning) callWindowUntil = System.currentTimeMillis() + 25_000
        main.post { speaker.say(prompt) }
    }
}
