package com.griffin.assistant

import android.content.Context
import android.content.Intent
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.speech.RecognitionListener
import android.speech.RecognizerIntent
import android.speech.SpeechRecognizer
import com.griffin.assistant.core.LEffect
import com.griffin.assistant.core.LError
import com.griffin.assistant.core.LEvent
import com.griffin.assistant.core.LState
import com.griffin.assistant.core.ListenerMachine

/**
 * Listens all the time, one utterance after another, and hands each finished sentence to [onHeard]. Every decision
 * (when to restart, what an error means, when to fall back from the on-device engine) lives in the pure
 * [ListenerMachine]; this class only owns the Android recogniser and the clock and runs the machine's effects.
 * It never touches any volume stream: the on-device engine has no start chime on most phones. Main thread only.
 */
class Listener(private val context: Context, private val onHeard: (List<String>) -> Unit, private val onStatus: (String) -> Unit, private val onLevel: (Float) -> Unit = {}) {
    private val main = Handler(Looper.getMainLooper())
    private var recognizer: SpeechRecognizer? = null
    private var state = LState()
    private val audioManager = context.getSystemService(Context.AUDIO_SERVICE) as? android.media.AudioManager

    val isActive get() = state.active

    fun start() = send(LEvent.Start)
    fun stop() = send(LEvent.Stop)
    /** Stops hearing while the assistant itself is talking, so it does not answer its own voice. */
    fun pause() = send(LEvent.Pause)
    fun resume() = send(LEvent.Resume)
    /** Forces a one-turn listen even if media or music is playing. */
    fun forceListen() = send(LEvent.ForceListen)

    private fun send(event: LEvent) {
        val step = ListenerMachine.reduce(state, event)
        state = step.state
        step.effects.forEach(::run)
    }

    private fun run(effect: LEffect) {
        when (effect) {
            LEffect.CancelTimers -> main.removeCallbacksAndMessages(null)
            LEffect.CancelListen -> try { recognizer?.cancel() } catch (_: Exception) {}
            LEffect.Destroy -> destroy()
            LEffect.BeginNow -> send(LEvent.BeginAttempt(audioManager?.isMusicActive == true))
            is LEffect.Schedule -> main.postDelayed({ send(LEvent.TimerFired(effect.recreate)) }, effect.delayMs)
            LEffect.StartRecognizer -> startRecognizer()
            is LEffect.Status -> onStatus(effect.text)
            is LEffect.Heard -> onHeard(effect.texts)
        }
    }

    private fun destroy() {
        try { recognizer?.destroy() } catch (_: Exception) {}
        recognizer = null
    }

    private fun onDeviceUsable() = android.os.Build.VERSION.SDK_INT >= 33 && SpeechRecognizer.isOnDeviceRecognitionAvailable(context)

    private fun create(): SpeechRecognizer? {
        if (!SpeechRecognizer.isRecognitionAvailable(context)) {
            onStatus("no speech recognition service installed on device")
            return null
        }
        val r = try {
            if (state.preferOnDevice && onDeviceUsable()) SpeechRecognizer.createOnDeviceSpeechRecognizer(context)
            else SpeechRecognizer.createSpeechRecognizer(context)
        } catch (_: Exception) {
            state = state.copy(preferOnDevice = false)
            try { SpeechRecognizer.createSpeechRecognizer(context) } catch (_: Exception) { null }
        }
        r?.setRecognitionListener(object : RecognitionListener {
            override fun onReadyForSpeech(params: Bundle?) = send(LEvent.Ready)
            override fun onBeginningOfSpeech() = send(LEvent.SpeechBegan)
            override fun onRmsChanged(rmsdB: Float) = onLevel(((rmsdB + 2f) / 12f).coerceIn(0f, 1f))
            override fun onBufferReceived(buffer: ByteArray?) {}
            override fun onEndOfSpeech() {}
            override fun onPartialResults(partialResults: Bundle?) {}
            override fun onEvent(eventType: Int, params: Bundle?) {}
            override fun onResults(results: Bundle?) =
                send(LEvent.Results(results?.getStringArrayList(SpeechRecognizer.RESULTS_RECOGNITION) ?: emptyList()))
            override fun onError(error: Int) = send(LEvent.Error(kindOf(error), onDeviceUsable()))
        })
        return r
    }

    private fun kindOf(code: Int) = when (code) {
        SpeechRecognizer.ERROR_NO_MATCH, SpeechRecognizer.ERROR_SPEECH_TIMEOUT -> LError.Silence
        SpeechRecognizer.ERROR_RECOGNIZER_BUSY -> LError.Busy
        SpeechRecognizer.ERROR_INSUFFICIENT_PERMISSIONS -> LError.Permission
        SpeechRecognizer.ERROR_LANGUAGE_NOT_SUPPORTED, SpeechRecognizer.ERROR_LANGUAGE_UNAVAILABLE -> LError.Language
        SpeechRecognizer.ERROR_CLIENT -> LError.Client
        SpeechRecognizer.ERROR_NETWORK, SpeechRecognizer.ERROR_NETWORK_TIMEOUT -> LError.Network
        SpeechRecognizer.ERROR_AUDIO -> LError.Audio
        SpeechRecognizer.ERROR_SERVER, SpeechRecognizer.ERROR_SERVER_DISCONNECTED -> LError.Server
        else -> LError.Other
    }

    private fun startRecognizer() {
        if (recognizer == null) recognizer = create()
        val r = recognizer
        if (r == null) { send(LEvent.NoRecognizer); return }
        val intent = Intent(RecognizerIntent.ACTION_RECOGNIZE_SPEECH).apply {
            putExtra(RecognizerIntent.EXTRA_LANGUAGE_MODEL, RecognizerIntent.LANGUAGE_MODEL_FREE_FORM)
            // Support Indian English and Hindi alongside default locale
            putExtra(RecognizerIntent.EXTRA_LANGUAGE, "en-IN")
            putExtra(RecognizerIntent.EXTRA_LANGUAGE_PREFERENCE, "en-IN")
            putExtra("android.speech.extra.EXTRA_ADDITIONAL_LANGUAGES", arrayOf("en-IN", "hi-IN", "en-US"))
            if (state.preferOffline) putExtra(RecognizerIntent.EXTRA_PREFER_OFFLINE, true)
            putExtra(RecognizerIntent.EXTRA_MAX_RESULTS, 5)
            putExtra(RecognizerIntent.EXTRA_SPEECH_INPUT_COMPLETE_SILENCE_LENGTH_MILLIS, 5000L)
            putExtra(RecognizerIntent.EXTRA_SPEECH_INPUT_POSSIBLY_COMPLETE_SILENCE_LENGTH_MILLIS, 5000L)
            putExtra(RecognizerIntent.EXTRA_CALLING_PACKAGE, context.packageName)
        }
        try { r.startListening(intent) } catch (e: Exception) { send(LEvent.StartFailed(e.message ?: "unknown")) }
    }
}
