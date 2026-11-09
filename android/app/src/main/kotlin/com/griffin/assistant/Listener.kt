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
class Listener(private val context: Context, private val onHeard: (String) -> Unit, private val onStatus: (String) -> Unit, private val onLevel: (Float) -> Unit = {}) {
    private val main = Handler(Looper.getMainLooper())
    private var recognizer: SpeechRecognizer? = null
    private var active = false
    private var paused = false
    private var restartPending = false

    val isActive get() = active

    fun start() {
        if (active) return
        active = true
        paused = false
        begin()
    }

    fun stop() {
        active = false
        main.removeCallbacksAndMessages(null)
        destroy()
        onStatus("stopped")
    }

    /** Stops hearing while the assistant itself is talking, so it does not answer its own voice. */
    fun pause() {
        paused = true
        main.removeCallbacksAndMessages(null)
        restartPending = false
        recognizer?.cancel()
    }

    fun resume() {
        if (!paused) return
        paused = false
        if (active) schedule(350)
    }

    private fun destroy() {
        recognizer?.destroy()
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
        // Only the recogniser that runs on the phone is used. The ordinary one can stream audio to a server, and an
        // always-on microphone must never do that: waiting for "hey" stays entirely on the device.
        val r = if (android.os.Build.VERSION.SDK_INT >= 33 && SpeechRecognizer.isOnDeviceRecognitionAvailable(context)) SpeechRecognizer.createOnDeviceSpeechRecognizer(context) else null
        r?.setRecognitionListener(object : RecognitionListener {
            override fun onReadyForSpeech(params: Bundle?) = onStatus("listening")
            override fun onBeginningOfSpeech() = onStatus("hearing you")
            override fun onRmsChanged(rmsdB: Float) = onLevel(((rmsdB + 2f) / 12f).coerceIn(0f, 1f))
            override fun onBufferReceived(buffer: ByteArray?) {}
            override fun onEndOfSpeech() {}
            override fun onPartialResults(partialResults: Bundle?) {}
            override fun onEvent(eventType: Int, params: Bundle?) {}
            override fun onResults(results: Bundle?) {
                val text = results?.getStringArrayList(SpeechRecognizer.RESULTS_RECOGNITION)?.firstOrNull()?.trim().orEmpty()
                if (text.isNotEmpty()) onHeard(text)
                schedule(250)
            }
            override fun onError(error: Int) {
                when (error) {
                    SpeechRecognizer.ERROR_NO_MATCH, SpeechRecognizer.ERROR_SPEECH_TIMEOUT -> schedule(150) // silence: the normal case
                    SpeechRecognizer.ERROR_RECOGNIZER_BUSY -> schedule(900, recreate = true)
                    SpeechRecognizer.ERROR_INSUFFICIENT_PERMISSIONS -> { active = false; onStatus("microphone permission is missing") }
                    SpeechRecognizer.ERROR_LANGUAGE_NOT_SUPPORTED, SpeechRecognizer.ERROR_LANGUAGE_UNAVAILABLE -> { onStatus("the speech language is unavailable"); schedule(5000, recreate = true) }
                    else -> { onStatus("speech error $error, retrying"); schedule(2000, recreate = true) }
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
            onStatus("needs the offline speech pack: install it in the Google app, Settings, Voice, Offline speech recognition")
            return
        }
        val intent = Intent(RecognizerIntent.ACTION_RECOGNIZE_SPEECH).apply {
            putExtra(RecognizerIntent.EXTRA_LANGUAGE_MODEL, RecognizerIntent.LANGUAGE_MODEL_FREE_FORM)
            putExtra(RecognizerIntent.EXTRA_PREFER_OFFLINE, true)
            putExtra(RecognizerIntent.EXTRA_MAX_RESULTS, 1)
            putExtra(RecognizerIntent.EXTRA_SPEECH_INPUT_COMPLETE_SILENCE_LENGTH_MILLIS, 1300L)
            putExtra(RecognizerIntent.EXTRA_CALLING_PACKAGE, context.packageName)
        }
        try {
            r.startListening(intent)
        } catch (e: Exception) {
            onStatus("could not start listening: ${e.message}")
            schedule(2000, recreate = true)
        }
    }
}
