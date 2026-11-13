package com.griffin.assistant

import android.animation.ValueAnimator
import android.content.Context
import android.graphics.Canvas
import android.graphics.LinearGradient
import android.graphics.Paint
import android.graphics.RadialGradient
import android.graphics.Shader
import android.view.View
import android.view.animation.LinearInterpolator
import androidx.core.content.ContextCompat
import kotlin.math.sin

/**
 * The floating ball: the Griffin mark on a dark glass disc with a violet-to-pink rim. It rests quietly, glows while
 * the assistant listens, and breathes while it thinks or speaks. Tapping it is the same as saying the wake phrase.
 */
class BallView(context: Context) : View(context) {
    var mode = OrbView.Mode.Hidden
        set(v) {
            field = v
            if (v == OrbView.Mode.Hidden) clock.cancel() else if (!clock.isRunning) clock.start()
            invalidate()
        }

    /** 0..1, set by the layer that drags the ball, to make it feel lifted. */
    var lift = 0f
        set(v) { field = v; invalidate() }

    private val d = resources.displayMetrics.density
    private var phase = 0f
    private val mark = ContextCompat.getDrawable(context, R.drawable.ic_griffin_mark)!!.mutate()
    private val disc = Paint(Paint.ANTI_ALIAS_FLAG)
    private val rim = Paint(Paint.ANTI_ALIAS_FLAG).apply { style = Paint.Style.STROKE; strokeWidth = 1.5f * d }
    private val glow = Paint(Paint.ANTI_ALIAS_FLAG)
    private val clock = ValueAnimator.ofFloat(0f, 1f).apply {
        duration = 1600
        repeatCount = ValueAnimator.INFINITE
        interpolator = LinearInterpolator()
        addUpdateListener { phase += 0.07f; invalidate() }
    }

    override fun onDetachedFromWindow() { clock.cancel(); super.onDetachedFromWindow() }

    override fun onDraw(c: Canvas) {
        val cx = width / 2f
        val cy = height / 2f
        val half = minOf(width, height) / 2f
        val r = half - 14f * d // leave room for the glow, which must stay inside the window or it is cut square
        val active = mode == OrbView.Mode.Listening
        val breathing = mode == OrbView.Mode.Thinking || mode == OrbView.Mode.Speaking
        val beat = if (active || breathing) (sin(phase * (if (active) 1.6f else 1f)) + 1f) / 2f else 0f

        // Soft coloured glow behind the disc; stronger while it listens.
        val glowR = minOf(half, r + (6f + 8f * beat * (if (active) 1f else 0.5f)) * d)
        val glowA = if (active) 0.55f + 0.35f * beat else if (breathing) 0.28f + 0.2f * beat else 0.16f + 0.1f * lift
        glow.shader = RadialGradient(cx, cy, glowR, intArrayOf(withAlpha(0xFF7C5CFF.toInt(), glowA), withAlpha(0xFFF062B4.toInt(), glowA * 0.4f), 0), floatArrayOf(0.55f, 0.8f, 1f), Shader.TileMode.CLAMP)
        c.drawCircle(cx, cy, glowR, glow)

        // The disc: near-black glass with a faint lighter top.
        val rr = r * (1f + 0.04f * lift)
        disc.shader = LinearGradient(cx, cy - rr, cx, cy + rr, 0xFF1B1D2B.toInt(), 0xFF0B0C14.toInt(), Shader.TileMode.CLAMP)
        c.drawCircle(cx, cy, rr, disc)
        rim.shader = LinearGradient(cx - rr, cy - rr, cx + rr, cy + rr, intArrayOf(0xFF8F6BFF.toInt(), 0x66C45CE6, 0xFFFF6FB1.toInt()), null, Shader.TileMode.CLAMP)
        c.drawCircle(cx, cy, rr, rim)

        // The mark, scaled so the ring fills the disc.
        val s = (rr * 2f * 1.18f).toInt()
        mark.setBounds((cx - s / 2f).toInt(), (cy - s / 2f).toInt(), (cx + s / 2f).toInt(), (cy + s / 2f).toInt())
        mark.alpha = if (mode == OrbView.Mode.Hidden) 230 else 255
        mark.draw(c)
    }

    private fun withAlpha(color: Int, a: Float): Int = (color and 0x00FFFFFF) or ((a.coerceIn(0f, 1f) * 255).toInt() shl 24)
}
