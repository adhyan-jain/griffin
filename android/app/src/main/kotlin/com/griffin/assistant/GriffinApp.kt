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

    /** The running assistant's door for events from outside it (a call ringing). Null while it is not running. */
    @Volatile var assistantEvents: ((com.griffin.assistant.core.AEvent) -> Unit)? = null
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

    /** What the assistant is doing, for the animation. Idle means it is only waiting for "hey" and the name. */
    @Volatile var mode = OrbView.Mode.Hidden
    @Volatile var level = 0f
    private val modeListeners = ArrayList<() -> Unit>()
    fun addModeListener(l: () -> Unit) { modeListeners += l }
    fun removeModeListener(l: () -> Unit) { modeListeners -= l }
    private fun notifyMode() = modeListeners.toList().forEach { it() }

    fun showMode(m: OrbView.Mode) { main.post { mode = m; notifyMode() } }
    fun showLevel(l: Float) { main.post { level = l; notifyMode() } }

    /** Debug builds only, and only to the developer log: what the assistant itself said. Never shown or kept. */
    fun dlog(line: String) { if (BuildConfig.DEBUG) android.util.Log.d("Griffin", line) }

    /**
     * Status lines for the screen. They say what the assistant did, never what anyone said: sentences that did not
     * start with the wake phrase are not recorded or logged anywhere, and even the ones that did are not kept.
     */
    fun log(line: String) {
        if (BuildConfig.DEBUG) android.util.Log.i("Griffin", line)
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
        log("incoming call announced")
        assistantEvents?.let { send -> main.post { send(com.griffin.assistant.core.AEvent.CallRinging(System.currentTimeMillis())) } }
        main.post { speaker.say(prompt) }
    }
}
