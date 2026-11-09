package com.griffin.assistant

import android.content.Context
import android.media.AudioAttributes
import android.speech.tts.TextToSpeech
import android.speech.tts.UtteranceProgressListener
import android.os.Handler
import android.os.Looper
import java.util.UUID

/** Speaks with the phone's own voice. Calls made before the engine is ready are queued, not lost. */
class Speaker(context: Context) {
    private val main = Handler(Looper.getMainLooper())
    private var ready = false
    private val queue = ArrayDeque<Pair<String, (() -> Unit)?>>()
    private val callbacks = HashMap<String, (() -> Unit)?>()
    /** Called with true when speech starts and false when it ends, so listening can pause meanwhile. */
    var onSpeaking: ((Boolean) -> Unit)? = null
    private var tts: TextToSpeech? = null
    private var active = 0

    init {
        tts = TextToSpeech(context.applicationContext) { status ->
            if (status == TextToSpeech.SUCCESS) {
                tts?.apply {
                    setAudioAttributes(AudioAttributes.Builder().setUsage(AudioAttributes.USAGE_ASSISTANT).setContentType(AudioAttributes.CONTENT_TYPE_SPEECH).build())
                    setOnUtteranceProgressListener(object : UtteranceProgressListener() {
                        override fun onStart(id: String?) = main.post { if (active++ == 0) onSpeaking?.invoke(true) }.let { }
                        override fun onDone(id: String?) = finish(id)
                        @Deprecated("Deprecated in Java") override fun onError(id: String?) = finish(id)
                    })
                }
                ready = true
                while (queue.isNotEmpty()) queue.removeFirst().let { speakNow(it.first, it.second) }
            }
        }
    }

    private fun finish(id: String?) {
        main.post {
            if (active > 0 && --active == 0) onSpeaking?.invoke(false)
            callbacks.remove(id)?.invoke()
        }
    }

    private fun speakNow(text: String, done: (() -> Unit)?) {
        val id = UUID.randomUUID().toString()
        if (done != null) callbacks[id] = done
        if (tts?.speak(text, TextToSpeech.QUEUE_ADD, null, id) != TextToSpeech.SUCCESS) finish(id)
    }

    fun say(text: String, done: (() -> Unit)? = null) {
        if (text.isBlank()) { done?.invoke(); return }
        if (ready) speakNow(text, done) else queue.addLast(text to done)
    }

    fun stop() {
        queue.clear()
        tts?.stop()
        if (active > 0) { active = 0; onSpeaking?.invoke(false) }
        callbacks.clear()
    }
}
