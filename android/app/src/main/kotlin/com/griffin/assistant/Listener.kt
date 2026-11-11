package com.griffin.assistant

import android.content.Context
import android.content.Intent
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.speech.RecognitionListener
import android.speech.RecognizerIntent
import android.speech.SpeechRecognizer

/**
 * Listens all the time, one utterance after another, and hands each finished sentence to [onHeard]. It prefers
 * the phone's on-device recogniser (private and works offline) and restarts itself after the silences and
 * hiccups that speech recognisers produce constantly. Everything runs on the main thread.
 */
class Listener(private val context: Context, private val onHeard: (List<String>) -> Unit, private val onStatus: (String) -> Unit, private val onLevel: (Float) -> Unit = {}) {
    private val main = Handler(Looper.getMainLooper())
    private var recognizer: SpeechRecognizer? = null
    private var active = false
    private var paused = false
    private var restartPending = false

    private var preferOnDevice = false
    private var preferOffline = false
    private var consecutiveClientErrors = 0
    private val audioManager = context.getSystemService(Context.AUDIO_SERVICE) as? android.media.AudioManager
    private var originalMusicVolume: Int? = null

    private fun muteChime() {
        try {
            audioManager?.let { am ->
                if (!am.isMusicActive) {
                    val cur = am.getStreamVolume(android.media.AudioManager.STREAM_MUSIC)
                    if (originalMusicVolume == null) originalMusicVolume = cur
                    am.setStreamVolume(android.media.AudioManager.STREAM_MUSIC, 0, 0)
                }
            }
        } catch (_: Exception) {}
        try {
            audioManager?.adjustStreamVolume(android.media.AudioManager.STREAM_SYSTEM, android.media.AudioManager.ADJUST_MUTE, 0)
        } catch (_: Exception) {}
    }

    private fun restoreVolume() {
        try {
            audioManager?.let { am ->
                originalMusicVolume?.let { vol ->
                    am.setStreamVolume(android.media.AudioManager.STREAM_MUSIC, vol, 0)
                    originalMusicVolume = null
                }
            }
        } catch (_: Exception) {}
        try {
            audioManager?.adjustStreamVolume(android.media.AudioManager.STREAM_SYSTEM, android.media.AudioManager.ADJUST_UNMUTE, 0)
        } catch (_: Exception) {}
    }

    val isActive get() = active

    fun start() {
        if (active) return
        active = true
        paused = false
        consecutiveClientErrors = 0
        begin()
    }

    fun stop() {
        active = false
        main.removeCallbacksAndMessages(null)
        restoreVolume()
        destroy()
        onStatus("stopped")
    }

    /** Stops hearing while the assistant itself is talking, so it does not answer its own voice. */
    fun pause() {
        paused = true
        main.removeCallbacksAndMessages(null)
        restartPending = false
        restoreVolume()
        try {
            recognizer?.cancel()
        } catch (_: Exception) {}
    }

    fun resume() {
        if (!paused) return
        paused = false
        if (active) schedule(350)
    }

    private fun destroy() {
        try {
            recognizer?.destroy()
        } catch (_: Exception) {}
        recognizer = null
    }

    private fun schedule(delayMs: Long, recreate: Boolean = false) {
        if (!active || paused || restartPending) return
        restartPending = true
        main.postDelayed({
            restartPending = false
            if (recreate) destroy()
            begin()
        }, delayMs)
    }

    private fun create(): SpeechRecognizer? {
        if (!SpeechRecognizer.isRecognitionAvailable(context)) {
            onStatus("no speech recognition service installed on device")
            return null
        }

        val r = try {
            if (preferOnDevice && android.os.Build.VERSION.SDK_INT >= 33 && SpeechRecognizer.isOnDeviceRecognitionAvailable(context)) {
                SpeechRecognizer.createOnDeviceSpeechRecognizer(context)
            } else {
                SpeechRecognizer.createSpeechRecognizer(context)
            }
        } catch (e: Exception) {
            if (preferOnDevice) {
                preferOnDevice = false
                try {
                    SpeechRecognizer.createSpeechRecognizer(context)
                } catch (_: Exception) {
                    null
                }
            } else {
                null
            }
        } ?: try {
            if (preferOnDevice) {
                preferOnDevice = false
                SpeechRecognizer.createSpeechRecognizer(context)
            } else null
        } catch (_: Exception) {
            null
        }

        r?.setRecognitionListener(object : RecognitionListener {
            override fun onReadyForSpeech(params: Bundle?) {
                restoreVolume()
                consecutiveClientErrors = 0
                onStatus("listening")
            }
            override fun onBeginningOfSpeech() {
                consecutiveClientErrors = 0
                onStatus("hearing you")
            }
            override fun onRmsChanged(rmsdB: Float) = onLevel(((rmsdB + 2f) / 12f).coerceIn(0f, 1f))
            override fun onBufferReceived(buffer: ByteArray?) {}
            override fun onEndOfSpeech() {}
            override fun onPartialResults(partialResults: Bundle?) {}
            override fun onEvent(eventType: Int, params: Bundle?) {}
            override fun onResults(results: Bundle?) {
                restoreVolume()
                consecutiveClientErrors = 0
                val list = results?.getStringArrayList(SpeechRecognizer.RESULTS_RECOGNITION)?.map { it.trim() }?.filter { it.isNotEmpty() } ?: emptyList()
                if (list.isNotEmpty()) onHeard(list)
                schedule(250)
            }
            override fun onError(error: Int) {
                restoreVolume()
                when (error) {
                    SpeechRecognizer.ERROR_NO_MATCH, SpeechRecognizer.ERROR_SPEECH_TIMEOUT -> schedule(400) // silence: the normal case
                    SpeechRecognizer.ERROR_RECOGNIZER_BUSY -> schedule(900, recreate = true)
                    SpeechRecognizer.ERROR_INSUFFICIENT_PERMISSIONS -> {
                        active = false
                        onStatus("microphone permission is missing")
                    }
                    SpeechRecognizer.ERROR_LANGUAGE_NOT_SUPPORTED, SpeechRecognizer.ERROR_LANGUAGE_UNAVAILABLE -> {
                        onStatus("the speech language is unavailable")
                        schedule(5000, recreate = true)
                    }
                    SpeechRecognizer.ERROR_CLIENT -> {
                        consecutiveClientErrors++
                        if (preferOnDevice && android.os.Build.VERSION.SDK_INT >= 33 && SpeechRecognizer.isOnDeviceRecognitionAvailable(context)) {
                            preferOnDevice = false
                            onStatus("on-device speech engine failed; falling back to standard engine")
                            schedule(500, recreate = true)
                        } else if (preferOffline) {
                            preferOffline = false
                            onStatus("offline speech engine failed; switching to online recognition")
                            schedule(500, recreate = true)
                        } else {
                            val delay = (2000L * consecutiveClientErrors).coerceAtMost(10000L)
                            onStatus("speech client error 5: check Google app mic permission & offline speech pack")
                            schedule(delay, recreate = true)
                        }
                    }
                    SpeechRecognizer.ERROR_NETWORK, SpeechRecognizer.ERROR_NETWORK_TIMEOUT -> {
                        onStatus("speech network error, retrying")
                        schedule(3000, recreate = true)
                    }
                    SpeechRecognizer.ERROR_AUDIO -> {
                        onStatus("audio recording error, retrying")
                        schedule(2000, recreate = true)
                    }
                    SpeechRecognizer.ERROR_SERVER, SpeechRecognizer.ERROR_SERVER_DISCONNECTED -> {
                        onStatus("speech server error, retrying")
                        schedule(3000, recreate = true)
                    }
                    else -> {
                        onStatus("speech error $error, retrying")
                        schedule(2000, recreate = true)
                    }
                }
            }
        })
        return r
    }

    private fun begin() {
        if (!active || paused) return
        if (recognizer == null) recognizer = create()
        val r = recognizer
        if (r == null) {
            onStatus("needs speech recognition service: install or enable Google speech recognition")
            schedule(5000, recreate = true)
            return
        }
        val intent = Intent(RecognizerIntent.ACTION_RECOGNIZE_SPEECH).apply {
            putExtra(RecognizerIntent.EXTRA_LANGUAGE_MODEL, RecognizerIntent.LANGUAGE_MODEL_FREE_FORM)
            // Support Indian English and Hindi alongside default locale
            putExtra(RecognizerIntent.EXTRA_LANGUAGE, "en-IN")
            putExtra(RecognizerIntent.EXTRA_LANGUAGE_PREFERENCE, "en-IN")
            putExtra("android.speech.extra.EXTRA_ADDITIONAL_LANGUAGES", arrayOf("en-IN", "hi-IN", "en-US"))
            if (preferOffline) {
                putExtra(RecognizerIntent.EXTRA_PREFER_OFFLINE, true)
            }
            putExtra(RecognizerIntent.EXTRA_MAX_RESULTS, 5)
            putExtra(RecognizerIntent.EXTRA_SPEECH_INPUT_COMPLETE_SILENCE_LENGTH_MILLIS, 5000L)
            putExtra(RecognizerIntent.EXTRA_SPEECH_INPUT_POSSIBLY_COMPLETE_SILENCE_LENGTH_MILLIS, 5000L)
            putExtra(RecognizerIntent.EXTRA_CALLING_PACKAGE, context.packageName)
        }
        try {
            muteChime()
            r.startListening(intent)
        } catch (e: Exception) {
            restoreVolume()
            onStatus("could not start listening: ${e.message}")
            schedule(2000, recreate = true)
        }
    }
}
