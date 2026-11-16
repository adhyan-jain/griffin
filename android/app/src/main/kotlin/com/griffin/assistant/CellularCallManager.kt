@file:Suppress("DEPRECATION")
package com.griffin.assistant

import android.app.NotificationChannel
import android.app.NotificationManager
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.media.AudioDeviceInfo
import android.media.AudioManager
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.speech.RecognitionListener
import android.speech.RecognizerIntent
import android.speech.SpeechRecognizer
import android.telecom.TelecomManager
import android.telephony.PhoneStateListener
import android.telephony.TelephonyCallback
import android.telephony.TelephonyManager
import androidx.annotation.RequiresApi
import androidx.core.app.NotificationCompat
import androidx.core.content.ContextCompat
import java.util.Locale
import java.util.UUID
import java.util.concurrent.atomic.AtomicBoolean

/**
 * Manages autonomous cellular phone calls made through the device's native SIM card.
 *
 * It dials the recipient via TelecomManager, monitors off-hook / answer state via TelephonyManager,
 * activates speakerphone, speaks the requested message using TTS, captures the recipient's verbatim
 * response using SpeechRecognizer, delivers a context fallback when questions are asked, hangs up,
 * notifies the owner, and records the interaction back to Griffin server.
 */
@Suppress("DEPRECATION")
class CellularCallManager(
    private val context: Context,
    private val api: ApiClient,
    private val speaker: Speaker,
) {
    private val main = Handler(Looper.getMainLooper())
    private val telecom get() = context.getSystemService(Context.TELECOM_SERVICE) as TelecomManager
    private val telephony get() = context.getSystemService(Context.TELEPHONY_SERVICE) as TelephonyManager
    private val audio get() = context.getSystemService(Context.AUDIO_SERVICE) as AudioManager
    private val prefs = context.getSharedPreferences("griffin", Context.MODE_PRIVATE)

    private val isCallInProgress = AtomicBoolean(false)
    private var prevAudioMode: Int = AudioManager.MODE_NORMAL
    private var prevSpeakerOn: Boolean = false

    @Volatile var currentRecipientName: String = ""
        private set
    @Volatile var currentNumber: String = ""
        private set
    @Volatile var isOffhook: Boolean = false
        private set
    @Volatile var isBackgroundCall: Boolean = false
        private set
    private var activeFinishCall: ((String) -> Unit)? = null

    val isCallActive: Boolean get() = isCallInProgress.get()

    private var telephonyCallback: Any? = null
    private var phoneStateListener: PhoneStateListener? = null
    private var activeRecognizer: SpeechRecognizer? = null

    companion object {
        const val CHANNEL_ID = "griffin_calls"
        const val CHANNEL_NAME = "Automated Calls"

        fun isQuestionOrUnknownContext(text: String): Boolean {
            if (text.isBlank()) return false
            if (text.contains("?")) return true
            val words = text.lowercase(Locale.ROOT).split(Regex("[^a-zA-Z0-9']+")).filter { it.isNotEmpty() }
            val questionWords = setOf("why", "where", "when", "what", "how", "who", "which", "whose")
            if (words.any { it in questionWords }) return true
            val lower = text.lowercase(Locale.ROOT)
            return lower.contains("what time") ||
                   lower.contains("tell me more") ||
                   lower.contains("which one") ||
                   lower.contains("how come") ||
                   lower.contains("kya") ||
                   lower.contains("kyu") ||
                   lower.contains("kyun") ||
                   lower.contains("kab") ||
                   lower.contains("kaha") ||
                   lower.contains("kaise")
        }

        fun buildGreeting(ownerName: String, message: String): String {
            return "Hello, this is Griffin calling on behalf of $ownerName. $message. Is there anything you would like me to convey back to $ownerName?"
        }

        fun buildFallback(ownerName: String): String {
            return "I will ask $ownerName and reply in a while, is there anything else you want to mention?"
        }

        fun buildClosing(ownerName: String): String {
            return "Thank you. I have noted that and will inform $ownerName. Goodbye."
        }

        fun ownerDisplayName(savedName: String?, email: String): String {
            val s = savedName?.trim().orEmpty()
            if (s.isNotEmpty()) return s
            if (email.contains("@")) {
                val prefix = email.substringBefore("@")
                val words = prefix.split('.', '_', '-').filter { it.isNotEmpty() }
                if (words.isNotEmpty()) {
                    return words.joinToString(" ") { word ->
                        word.replaceFirstChar { if (it.isLowerCase()) it.titlecase(Locale.ROOT) else it.toString() }
                    }
                }
            }
            return "the owner"
        }
    }

    fun startAutomatedCall(
        number: String,
        recipientName: String,
        message: String,
        callId: String = "",
        inBackground: Boolean = false,
        onDone: ((outcome: String, reply: String, followup: String) -> Unit)? = null,
    ): Boolean {
        if (!isCallInProgress.compareAndSet(false, true)) {
            onDone?.invoke("busy", "", "")
            return false
        }

        val hasCallPermission = ContextCompat.checkSelfPermission(context, android.Manifest.permission.CALL_PHONE) == PackageManager.PERMISSION_GRANTED
        val hasPhoneStatePermission = ContextCompat.checkSelfPermission(context, android.Manifest.permission.READ_PHONE_STATE) == PackageManager.PERMISSION_GRANTED
        if (!hasCallPermission || !hasPhoneStatePermission) {
            isCallInProgress.set(false)
            onDone?.invoke("failed", "Missing CALL_PHONE or READ_PHONE_STATE permission", "")
            return false
        }

        val actualCallId = callId.ifEmpty { UUID.randomUUID().toString() }
        val owner = ownerDisplayName(prefs.getString("owner_name", null), api.email)

        currentRecipientName = recipientName.ifEmpty { number }
        currentNumber = number
        isOffhook = false
        isBackgroundCall = inBackground
        if (inBackground) {
            GriffinAccess.suppressDialer = true
        }

        var offhookSeen = false
        var offhookTimestamp = 0L
        var replyRecorded = ""
        var followupRecorded = ""
        val callCompleted = AtomicBoolean(false)

        fun finishCall(outcome: String) {
            if (!callCompleted.compareAndSet(false, true)) return
            GriffinAccess.suppressDialer = false
            isOffhook = false
            isBackgroundCall = false
            activeFinishCall = null
            speaker.setInCallMode(false)
            cleanupCall()

            // Post notification to owner
            postOutcomeNotification(
                recipientName = recipientName.ifEmpty { number },
                message = message,
                outcome = outcome,
                reply = replyRecorded,
                followup = followupRecorded,
            )

            // Report call to server
            Thread {
                try {
                    api.recordCall(
                        to = number,
                        recipientName = recipientName,
                        outcome = outcome,
                        message = message,
                        reply = replyRecorded,
                        followup = followupRecorded,
                        callId = actualCallId,
                    )
                } catch (_: Exception) {}
            }.start()

            onDone?.invoke(outcome, replyRecorded, followupRecorded)
        }

        activeFinishCall = { finishCall(it) }

        // Set up telephony state listening
        registerCallStateListener(
            onOffhook = {
                if (offhookSeen) return@registerCallStateListener
                offhookSeen = true
                isOffhook = true
                offhookTimestamp = System.currentTimeMillis()

                enableSpeakerphone()
                speaker.setInCallMode(true)

                // Wait 3500ms after offhook for recipient connection & audio line stabilization
                main.postDelayed({
                    if (callCompleted.get()) return@postDelayed

                    val greeting = buildGreeting(owner, message)
                    speaker.say(greeting) {
                        if (callCompleted.get()) return@say
                        // Prompt finished speaking; listen for recipient's reply
                        listenForSpeech(
                            timeoutMs = 7000L,
                            onResult = { reply ->
                                replyRecorded = reply
                                if (isQuestionOrUnknownContext(reply)) {
                                    val fallback = buildFallback(owner)
                                    speaker.say(fallback) {
                                        if (callCompleted.get()) return@say
                                        listenForSpeech(
                                            timeoutMs = 6000L,
                                            onResult = { extra ->
                                                followupRecorded = extra
                                                speaker.say(buildClosing(owner)) {
                                                    main.postDelayed({
                                                        endCallSafely()
                                                        finishCall(if (replyRecorded.isNotBlank()) "replied" else "delivered")
                                                    }, 800)
                                                }
                                            }
                                        )
                                    }
                                } else {
                                    speaker.say(buildClosing(owner)) {
                                        main.postDelayed({
                                            endCallSafely()
                                            finishCall(if (replyRecorded.isNotBlank()) "replied" else "delivered")
                                        }, 800)
                                    }
                                }
                            }
                        )
                    }
                }, 3500)
            },
            onIdle = {
                if (!offhookSeen) {
                    // Initial registration callback while phone is idle before placeCall connects: IGNORE!
                    return@registerCallStateListener
                }
                val duration = System.currentTimeMillis() - offhookTimestamp
                if (duration < 3500L && replyRecorded.isBlank()) {
                    // Disconnected within 3.5 seconds of dialing -> rejected / busy / unanswered
                    finishCall("no_answer")
                } else {
                    finishCall(if (replyRecorded.isNotBlank()) "replied" else "delivered")
                }
            }
        )

        // Place the call via native SIM
        try {
            val uri = Uri.fromParts("tel", number, null)
            val callExtras = Bundle().apply {
                putBoolean(TelecomManager.EXTRA_START_CALL_WITH_SPEAKERPHONE, true)
            }
            telecom.placeCall(uri, callExtras)
        } catch (e: Exception) {
            finishCall("failed")
            return false
        }

        // Connection watchdog: If call never connects off-hook within 25 seconds, terminate
        main.postDelayed({
            if (!callCompleted.get() && !offhookSeen) {
                endCallSafely()
                finishCall("no_answer")
            }
        }, 25_000)

        // Fallback watchdog: If call is still ongoing after 180 seconds, forcibly terminate
        main.postDelayed({
            if (!callCompleted.get()) {
                endCallSafely()
                finishCall(if (replyRecorded.isNotBlank()) "replied" else "timeout")
            }
        }, 180_000)

        return true
    }

    /**
     * Immediately cancels or disconnects an ongoing or connecting call.
     * If not yet answered: cuts immediately.
     * If already answered: speaks an excuse over the call line ("change of plan") and then cuts.
     */
    fun cancelCurrentCall(who: String? = null, onEnded: ((String) -> Unit)? = null): Boolean {
        if (!isCallInProgress.get()) {
            onEnded?.invoke("No active call in progress.")
            return false
        }

        if (!who.isNullOrBlank() && currentRecipientName.isNotBlank()) {
            val target = who.trim().lowercase(Locale.ROOT)
            val current = currentRecipientName.trim().lowercase(Locale.ROOT)
            if (!current.contains(target) && !target.contains(current)) {
                onEnded?.invoke("Active call is with $currentRecipientName, not $who.")
                return false
            }
        }

        val name = currentRecipientName.ifEmpty { "the recipient" }

        // Immediately stop SpeechRecognizer if active
        try {
            activeRecognizer?.destroy()
        } catch (_: Exception) {}
        activeRecognizer = null

        if (!isOffhook) {
            // Call not yet answered: cut instantly
            endCallSafely()
            val finish = activeFinishCall
            finish?.invoke("cancelled_by_user") ?: cleanupCall()
            onEnded?.invoke("Call to $name cancelled before answer.")
            return true
        }

        // Call is already answered (recipient on the line)
        // Speak polite excuse before hanging up:
        val excuse = "Sorry, there might have been a change of plan, I need to drop off now. Talk soon, bye!"
        speaker.say(excuse) {
            main.postDelayed({
                endCallSafely()
                val finish = activeFinishCall
                finish?.invoke("cancelled_with_excuse") ?: cleanupCall()
                onEnded?.invoke("Excused for change of plan and ended call to $name.")
            }, 600)
        }
        return true
    }

    private fun enableSpeakerphone() {
        try {
            prevAudioMode = audio.mode
            prevSpeakerOn = audio.isSpeakerphoneOn
            audio.mode = AudioManager.MODE_IN_COMMUNICATION
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
                val speakerDevice = audio.availableCommunicationDevices.firstOrNull {
                    it.type == AudioDeviceInfo.TYPE_BUILTIN_SPEAKER
                }
                if (speakerDevice != null) {
                    audio.setCommunicationDevice(speakerDevice)
                }
            } else {
                audio.isSpeakerphoneOn = true
            }
        } catch (_: Exception) {}
    }

    private fun restoreAudio() {
        try {
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
                audio.clearCommunicationDevice()
            } else {
                audio.isSpeakerphoneOn = prevSpeakerOn
            }
            audio.mode = prevAudioMode
        } catch (_: Exception) {}
    }

    private fun endCallSafely() {
        try {
            telecom.endCall()
        } catch (_: Exception) {}
    }

    private fun listenForSpeech(timeoutMs: Long, onResult: (String) -> Unit) {
        main.post {
            if (!SpeechRecognizer.isRecognitionAvailable(context)) {
                onResult("")
                return@post
            }

            var listenerFired = false
            fun deliverOnce(text: String) {
                if (listenerFired) return
                listenerFired = true
                try {
                    activeRecognizer?.destroy()
                } catch (_: Exception) {}
                activeRecognizer = null
                onResult(text.trim())
            }

            val timeoutRunnable = Runnable { deliverOnce("") }
            main.postDelayed(timeoutRunnable, timeoutMs)

            try {
                val recognizer = SpeechRecognizer.createSpeechRecognizer(context)
                activeRecognizer = recognizer
                recognizer.setRecognitionListener(object : RecognitionListener {
                    override fun onReadyForSpeech(params: Bundle?) {}
                    override fun onBeginningOfSpeech() {}
                    override fun onRmsChanged(rmsdB: Float) {}
                    override fun onBufferReceived(buffer: ByteArray?) {}
                    override fun onEndOfSpeech() {}
                    override fun onPartialResults(partialResults: Bundle?) {}
                    override fun onEvent(eventType: Int, params: Bundle?) {}
                    override fun onResults(results: Bundle?) {
                        main.removeCallbacks(timeoutRunnable)
                        val matches = results?.getStringArrayList(SpeechRecognizer.RESULTS_RECOGNITION)
                        val text = matches?.firstOrNull().orEmpty()
                        deliverOnce(text)
                    }
                    override fun onError(error: Int) {
                        main.removeCallbacks(timeoutRunnable)
                        deliverOnce("")
                    }
                })

                val intent = Intent(RecognizerIntent.ACTION_RECOGNIZE_SPEECH).apply {
                    putExtra(RecognizerIntent.EXTRA_LANGUAGE_MODEL, RecognizerIntent.LANGUAGE_MODEL_FREE_FORM)
                    putExtra(RecognizerIntent.EXTRA_LANGUAGE, "en-IN")
                    putExtra(RecognizerIntent.EXTRA_MAX_RESULTS, 3)
                    putExtra(RecognizerIntent.EXTRA_SPEECH_INPUT_COMPLETE_SILENCE_LENGTH_MILLIS, 3500L)
                    putExtra(RecognizerIntent.EXTRA_SPEECH_INPUT_POSSIBLY_COMPLETE_SILENCE_LENGTH_MILLIS, 3500L)
                }
                recognizer.startListening(intent)
            } catch (e: Exception) {
                main.removeCallbacks(timeoutRunnable)
                deliverOnce("")
            }
        }
    }

    private fun registerCallStateListener(onOffhook: () -> Unit, onIdle: () -> Unit) {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
            registerTelephonyCallbackS(onOffhook, onIdle)
        } else {
            registerPhoneStateListenerLegacy(onOffhook, onIdle)
        }
    }

    @RequiresApi(Build.VERSION_CODES.S)
    private fun registerTelephonyCallbackS(onOffhook: () -> Unit, onIdle: () -> Unit) {
        val callback = object : TelephonyCallback(), TelephonyCallback.CallStateListener {
            override fun onCallStateChanged(state: Int) {
                when (state) {
                    TelephonyManager.CALL_STATE_OFFHOOK -> onOffhook()
                    TelephonyManager.CALL_STATE_IDLE -> onIdle()
                }
            }
        }
        telephonyCallback = callback
        try {
            telephony.registerTelephonyCallback(context.mainExecutor, callback)
        } catch (_: Exception) {}
    }

    @Suppress("DEPRECATION")
    private fun registerPhoneStateListenerLegacy(onOffhook: () -> Unit, onIdle: () -> Unit) {
        val listener = object : PhoneStateListener() {
            @Deprecated("Deprecated in Java")
            override fun onCallStateChanged(state: Int, phoneNumber: String?) {
                when (state) {
                    TelephonyManager.CALL_STATE_OFFHOOK -> onOffhook()
                    TelephonyManager.CALL_STATE_IDLE -> onIdle()
                }
            }
        }
        phoneStateListener = listener
        try {
            telephony.listen(listener, PhoneStateListener.LISTEN_CALL_STATE)
        } catch (_: Exception) {}
    }

    private fun unregisterCallStateListener() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
            (telephonyCallback as? TelephonyCallback)?.let {
                try { telephony.unregisterTelephonyCallback(it) } catch (_: Exception) {}
            }
            telephonyCallback = null
        } else {
            phoneStateListener?.let {
                try {
                    @Suppress("DEPRECATION")
                    telephony.listen(it, PhoneStateListener.LISTEN_NONE)
                } catch (_: Exception) {}
            }
            phoneStateListener = null
        }
    }

    private fun cleanupCall() {
        restoreAudio()
        unregisterCallStateListener()
        try {
            activeRecognizer?.destroy()
        } catch (_: Exception) {}
        activeRecognizer = null
        isCallInProgress.set(false)
    }

    private fun postOutcomeNotification(
        recipientName: String,
        message: String,
        outcome: String,
        reply: String,
        followup: String,
    ) {
        try {
            val nm = context.getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
                val channel = NotificationChannel(CHANNEL_ID, CHANNEL_NAME, NotificationManager.IMPORTANCE_HIGH).apply {
                    description = "Outcomes and replies from automated phone calls"
                    enableVibration(true)
                }
                nm.createNotificationChannel(channel)
            }

            val title = "Call to $recipientName"
            val summary = when {
                reply.isNotBlank() -> "Recipient: \"$reply\""
                outcome == "no_answer" -> "Call was not answered"
                outcome == "delivered" -> "Message delivered"
                else -> "Call ended ($outcome)"
            }

            val bigText = buildString {
                append("Message: $message\n")
                if (reply.isNotBlank()) append("Reply: \"$reply\"\n")
                if (followup.isNotBlank()) append("Notes: \"$followup\"\n")
                append("Outcome: $outcome")
            }

            val notif = NotificationCompat.Builder(context, CHANNEL_ID)
                .setSmallIcon(R.drawable.ic_stat_griffin)
                .setContentTitle(title)
                .setContentText(summary)
                .setStyle(NotificationCompat.BigTextStyle().bigText(bigText))
                .setPriority(NotificationCompat.PRIORITY_HIGH)
                .setAutoCancel(true)
                .build()

            nm.notify(UUID.randomUUID().hashCode(), notif)
        } catch (_: Exception) {}
    }
}
