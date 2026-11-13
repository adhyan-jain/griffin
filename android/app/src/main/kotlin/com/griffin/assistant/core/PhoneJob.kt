package com.griffin.assistant.core

import org.json.JSONArray
import org.json.JSONObject
import java.security.MessageDigest

/**
 * Using any app on the phone, one step at a time, stopping BEFORE anything is sent. This file is the pure part
 * (the loop, the safety rules and the script digest); the screen and the network are passed in, so it is tested
 * without a phone. It mirrors internal/hands/browser on the server, and [PhoneScript.digest] must produce the
 * same value as domain.ScriptDigest in Go, because the owner's approval is bound to it.
 */
data class PStep(
    val kind: String, val ref: String = "", val role: String = "", val name: String = "", val text: String = "",
    val url: String = "", val note: String = "", val final: Boolean = false,
)

data class PElement(val ref: String, val role: String, val name: String, val value: String = "")
data class PScreen(val app: String, val title: String, val text: String, val elements: List<PElement>) {
    fun find(ref: String) = elements.firstOrNull { it.ref == ref }
}
data class PDecision(val done: Boolean, val result: String, val step: PStep)

interface PScreenDriver {
    fun snapshot(): PScreen?
    /** Performs one step; null on success, or what went wrong. */
    fun perform(step: PStep): String?
}

interface PBrain { fun next(goal: String, screen: PScreen, done: List<PStep>): PDecision }

sealed class PRun {
    data class Answer(val result: String) : PRun()
    data class Staged(val steps: List<PStep>, val digest: String, val preview: String) : PRun()
    data class Failed(val reason: String) : PRun()
}

object PhoneScript {
    fun digest(goal: String, steps: List<PStep>): String {
        val md = MessageDigest.getInstance("SHA-256")
        md.update(goal.toByteArray(Charsets.UTF_8))
        for (s in steps) {
            md.update(byteArrayOf(0x1f))
            md.update(listOf(s.kind, s.ref, s.role, s.name, s.text, s.url, s.final.toString()).joinToString("\u001e").toByteArray(Charsets.UTF_8))
        }
        return md.digest().take(16).joinToString("") { "%02x".format(it) }
    }

    fun approvalType(digest: String) = "host_action:browser:$digest"

    private val words = Regex("\\b(send|submit|post|reply|apply|confirm|pay|buy|purchase|order|checkout|delete|remove|publish|connect|invite|accept|place|sign ?(in|up)?|subscribe|share|tweet|follow|donate|transfer|withdraw)\\b", RegexOption.IGNORE_CASE)

    /** Anything that could send, spend or destroy. Errs toward asking, and the code decides whatever the model says. */
    fun irreversible(s: PStep) = when (s.kind) {
        "click" -> words.containsMatchIn(s.name)
        "press" -> s.text.equals("enter", true)
        else -> false
    }

    fun toJson(steps: List<PStep>): JSONArray = JSONArray().apply {
        steps.forEach { put(JSONObject().put("kind", it.kind).put("ref", it.ref).put("role", it.role).put("name", it.name).put("text", it.text).put("url", it.url).put("note", it.note).put("final", it.final)) }
    }

    fun fromJson(arr: JSONArray): List<PStep> = (0 until arr.length()).map {
        val o = arr.getJSONObject(it)
        PStep(o.optString("kind"), o.optString("ref"), o.optString("role"), o.optString("name"), o.optString("text"), o.optString("url"), o.optString("note"), o.optBoolean("final"))
    }

    /** Works toward [goal]. A step marked (or found to be) final is never performed: the run returns it, staged. */
    fun run(goal: String, screen: PScreenDriver, brain: PBrain, maxSteps: Int = 25): PRun {
        val steps = mutableListOf<PStep>()
        repeat(maxSteps) { i ->
            val view = screen.snapshot() ?: return PRun.Failed("the screen could not be read (is the accessibility service on, and the phone unlocked?)")
            val d = try { brain.next(goal, view, steps) } catch (e: Exception) { return PRun.Failed("could not choose the next step: ${e.message}") }
            if (d.done) return PRun.Answer(d.result)
            var step = d.step
            if (step.kind == "click" || step.kind == "type") {
                val el = view.find(step.ref) ?: return PRun.Failed("the agent chose element ${step.ref}, which is not on the screen")
                step = step.copy(role = el.role, name = el.name)
            }
            if (irreversible(step)) step = step.copy(final = true)
            steps += step
            if (step.final) {
                val label = view.find(step.ref)?.let { "${it.role} “${it.name}”" } ?: step.ref
                val preview = "On ${view.title.ifEmpty { view.app }}\n${view.text.take(1200)}\n\nNext, and last: ${step.kind} $label. ${step.note}".trim()
                return PRun.Staged(steps.toList(), digest(goal, steps), preview)
            }
            screen.perform(step)?.let { return PRun.Failed("step ${i + 1} (${step.kind}): $it") }
        }
        return PRun.Failed("the phone job ran out of steps before it finished")
    }

    /** Replays an approved script exactly, including its final step, with no model involved. */
    fun replay(screen: PScreenDriver, steps: List<PStep>): String? {
        if (steps.isEmpty() || steps.dropLast(1).any { it.final }) return "the script is not valid"
        steps.forEachIndexed { i, s -> screen.perform(s)?.let { return "step ${i + 1} (${s.kind}): $it" } }
        return null
    }
}
