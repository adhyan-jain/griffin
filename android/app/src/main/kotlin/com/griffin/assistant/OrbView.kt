package com.griffin.assistant

import android.animation.ValueAnimator
import android.content.Context
import android.graphics.Canvas
import android.graphics.LinearGradient
import android.graphics.Paint
import android.graphics.Path
import android.graphics.Shader
import android.view.View
import android.view.animation.LinearInterpolator
import kotlin.math.PI
import kotlin.math.abs
import kotlin.math.sin

/**
 * The "I am listening" animation: soft coloured waves along the bottom edge that swell with your voice while it
 * listens, drift slowly while it thinks, and pulse while it speaks. It draws nothing at all when idle, so an idle
 * assistant is visually silent.
 */
class OrbView(context: Context) : View(context) {
    enum class Mode { Hidden, Listening, Thinking, Speaking }

    var mode = Mode.Hidden
        set(v) {
            field = v
            visibility = if (v == Mode.Hidden) INVISIBLE else VISIBLE
            if (v != Mode.Hidden) startClock() else stopClock()
            invalidate()
        }

    /** 0..1, how loud the owner is. */
    var level = 0f
        set(v) {
            field = maxOf(v.coerceIn(0f, 1f), field * 0.82f) // swell quickly, settle gently
        }

    private var phase = 0f
    private var pulse = 0f
    private val paints = Array(3) { Paint(Paint.ANTI_ALIAS_FLAG).apply { style = Paint.Style.FILL } }
    private val path = Path()
    private val clock = ValueAnimator.ofFloat(0f, 1f).apply {
        duration = 2400
        repeatCount = ValueAnimator.INFINITE
        interpolator = LinearInterpolator()
        addUpdateListener {
            val speed = when (mode) { Mode.Listening -> 1.0f; Mode.Thinking -> 0.35f; else -> 0.6f }
            phase += 0.09f * speed
            pulse = (sin(phase * 1.7f) + 1f) / 2f
            level *= 0.97f
            invalidate()
        }
    }

    private fun startClock() { if (!clock.isRunning) clock.start() }
    private fun stopClock() { clock.cancel() }

    override fun onDetachedFromWindow() {
        stopClock()
        super.onDetachedFromWindow()
    }

    override fun onDraw(canvas: Canvas) {
        if (mode == Mode.Hidden) return
        val w = width.toFloat()
        val h = height.toFloat()
        if (w <= 0f || h <= 0f) return
        val energy = when (mode) {
            Mode.Listening -> 0.30f + 0.70f * level
            Mode.Thinking -> 0.22f
            Mode.Speaking -> 0.28f + 0.30f * pulse
            Mode.Hidden -> 0f
        }
        val colours = arrayOf(
            intArrayOf(0x00_5B8CFF, 0xCC_5B8CFF.toInt(), 0x00_5B8CFF),
            intArrayOf(0x00_B45BFF, 0xB0_B45BFF.toInt(), 0x00_B45BFF),
            intArrayOf(0x00_FF6FA8, 0x99_FF6FA8.toInt(), 0x00_FF6FA8),
        )
        for (i in 0 until 3) {
            val amp = h * 0.42f * energy * (1f - i * 0.18f)
            val freq = (1.4f + i * 0.55f) * PI.toFloat() / w
            val offset = phase * (1f + i * 0.4f) * if (i % 2 == 0) 1f else -1f
            path.reset()
            path.moveTo(0f, h)
            var x = 0f
            while (x <= w) {
                val bell = 1f - abs(x / w * 2f - 1f).let { it * it } // tallest in the middle, flat at the edges
                path.lineTo(x, h - amp * bell * (0.55f + 0.45f * sin(x * freq + offset)) - h * 0.04f)
                x += 8f
            }
            path.lineTo(w, h)
            path.close()
            paints[i].shader = LinearGradient(0f, 0f, w, 0f, colours[i], floatArrayOf(0f, 0.5f, 1f), Shader.TileMode.CLAMP)
            canvas.drawPath(path, paints[i])
        }
    }
}
