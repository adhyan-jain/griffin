package com.griffin.assistant

import android.content.Context
import android.media.AudioAttributes
import android.media.AudioDeviceInfo
import android.media.AudioFocusRequest
import android.media.AudioManager
import android.media.MediaPlayer
import android.os.Build
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.speech.tts.TextToSpeech
import android.speech.tts.UtteranceProgressListener
import java.io.File
import java.io.FileOutputStream
import java.util.UUID

/**
 * Speaks with JARVIS-quality voice. Supports provider-based neural voice audio playback,
 * Bluetooth earbuds/headset routing, urgent interruption, and on-device TTS fallback.
 */
class Speaker(private val context: Context) {
    private val audio = context.applicationContext.getSystemService(Context.AUDIO_SERVICE) as AudioManager
    private val speechAttrs = AudioAttributes.Builder()
        .setUsage(AudioAttributes.USAGE_ASSISTANT)
        .setContentType(AudioAttributes.CONTENT_TYPE_SPEECH)
        .build()
    private val focus = AudioFocusRequest.Builder(AudioManager.AUDIOFOCUS_GAIN_TRANSIENT_MAY_DUCK)
        .setAudioAttributes(speechAttrs)
        .setWillPauseWhenDucked(false)
        .build()
    private var holdingFocus = false

    /** Asks other audio (a game, a video, music) to turn down while this speaks, never to stop. */
    private fun takeFocus() {
        if (!holdingFocus) holdingFocus = audio.requestAudioFocus(focus) == AudioManager.AUDIOFOCUS_REQUEST_GRANTED
    }
    private fun giveFocus() {
        if (holdingFocus) {
            audio.abandonAudioFocusRequest(focus)
            holdingFocus = false
        }
    }

    private val main = Handler(Looper.getMainLooper())
    private var ready = false
    private val queue = ArrayDeque<Pair<String, (() -> Unit)?>>()
    private val callbacks = HashMap<String, (() -> Unit)?>()

    /** Called with true when speech starts and false when it ends, so listening can pause meanwhile. */
    var onSpeaking: ((Boolean) -> Unit)? = null
    private var tts: TextToSpeech? = null
    private var mediaPlayer: MediaPlayer? = null
    private var active = 0

    init {
        tts = TextToSpeech(context.applicationContext) { status ->
            if (status == TextToSpeech.SUCCESS) {
                tts?.apply {
                    setAudioAttributes(
                        AudioAttributes.Builder()
                            .setUsage(AudioAttributes.USAGE_ASSISTANT)
                            .setContentType(AudioAttributes.CONTENT_TYPE_SPEECH)
                            .build()
                    )
                    setOnUtteranceProgressListener(object : UtteranceProgressListener() {
                        override fun onStart(id: String?) = main.post {
                            if (active++ == 0) {
                                takeFocus()
                                onSpeaking?.invoke(true)
                            }
                        }.let { }
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
            if (active > 0 && --active == 0) {
                giveFocus()
                onSpeaking?.invoke(false)
            }
            callbacks.remove(id)?.invoke()
        }
    }

    /** Plays neural voice MP3 bytes directly through MediaPlayer with earbud routing. */
    fun playNeuralAudio(audioBytes: ByteArray, done: (() -> Unit)? = null) {
        if (audioBytes.isEmpty()) {
            done?.invoke()
            return
        }
        main.post {
            try {
                stop()
                takeFocus()
                active++
                onSpeaking?.invoke(true)

                val tempFile = File.createTempFile("griffin_tts_", ".mp3", context.cacheDir)
                FileOutputStream(tempFile).use { it.write(audioBytes) }

                mediaPlayer = MediaPlayer().apply {
                    setAudioAttributes(speechAttrs)
                    setDataSource(tempFile.absolutePath)
                    routeToPreferredDevice(this)
                    setOnCompletionListener {
                        tempFile.delete()
                        mediaPlayer?.release()
                        mediaPlayer = null
                        if (active > 0 && --active == 0) {
                            giveFocus()
                            onSpeaking?.invoke(false)
                        }
                        done?.invoke()
                    }
                    setOnErrorListener { _, _, _ ->
                        tempFile.delete()
                        mediaPlayer?.release()
                        mediaPlayer = null
                        if (active > 0 && --active == 0) {
                            giveFocus()
                            onSpeaking?.invoke(false)
                        }
                        done?.invoke()
                        true
                    }
                    prepare()
                    start()
                }
            } catch (_: Exception) {
                if (active > 0 && --active == 0) {
                    giveFocus()
                    onSpeaking?.invoke(false)
                }
                done?.invoke()
            }
        }
    }

    private fun routeToPreferredDevice(player: MediaPlayer) {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.M) {
            val devices = audio.getDevices(AudioManager.GET_DEVICES_OUTPUTS)
            // Prefer Bluetooth earbuds or wired headphones if connected
            val earbud = devices.firstOrNull {
                it.type == AudioDeviceInfo.TYPE_BLUETOOTH_A2DP ||
                it.type == AudioDeviceInfo.TYPE_BLUETOOTH_SCO ||
                it.type == AudioDeviceInfo.TYPE_WIRED_HEADSET ||
                it.type == AudioDeviceInfo.TYPE_WIRED_HEADPHONES
            }
            if (earbud != null) {
                player.setPreferredDevice(earbud)
            }
        }
    }

    private fun speakNow(text: String, done: (() -> Unit)?) {
        val id = UUID.randomUUID().toString()
        if (done != null) callbacks[id] = done
        val params = Bundle().apply {
            putFloat(TextToSpeech.Engine.KEY_PARAM_VOLUME, 1.0f)
            putInt(TextToSpeech.Engine.KEY_PARAM_STREAM, AudioManager.STREAM_MUSIC)
        }
        if (tts?.speak(text, TextToSpeech.QUEUE_ADD, params, id) != TextToSpeech.SUCCESS) finish(id)
    }

    fun setInCallMode(inCall: Boolean) {
        val usage = if (inCall) AudioAttributes.USAGE_MEDIA else AudioAttributes.USAGE_ASSISTANT
        try {
            tts?.setAudioAttributes(
                AudioAttributes.Builder()
                    .setUsage(usage)
                    .setContentType(AudioAttributes.CONTENT_TYPE_SPEECH)
                    .build()
            )
        } catch (_: Exception) {}
    }

    /** Ordinary speech request. */
    fun say(text: String, done: (() -> Unit)? = null) {
        if (text.isBlank()) {
            done?.invoke()
            return
        }
        if (ready) speakNow(text, done) else queue.addLast(text to done)
    }

    /** Urgent interruption: stops in-flight playback immediately and speaks without queue delay. */
    fun sayUrgent(text: String, done: (() -> Unit)? = null) {
        stop()
        say(text, done)
    }

    fun stop() {
        queue.clear()
        try {
            mediaPlayer?.stop()
            mediaPlayer?.release()
        } catch (_: Exception) {}
        mediaPlayer = null
        tts?.stop()
        if (active > 0) {
            active = 0
            giveFocus()
            onSpeaking?.invoke(false)
        }
        callbacks.clear()
    }
}
