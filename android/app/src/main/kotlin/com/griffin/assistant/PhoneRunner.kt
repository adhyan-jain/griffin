package com.griffin.assistant

import android.content.Context
import com.griffin.assistant.core.PBrain
import com.griffin.assistant.core.PDecision
import com.griffin.assistant.core.PRun
import com.griffin.assistant.core.PScreen
import com.griffin.assistant.core.PStep
import com.griffin.assistant.core.PhoneScript
import org.json.JSONObject
import java.util.concurrent.atomic.AtomicBoolean

/**
 * Runs one phone job at a time for the owner. It claims a task, works the goal on the screen through
 * [GriffinAccess] (the server chooses each step, so no model key lives here), and stops before anything is sent:
 * the prepared steps are kept on the phone under their digest and the owner is asked to approve exactly those.
 * When the owner approves, the next claim carries that approval and the steps are replayed as recorded.
 */
class PhoneRunner(context: Context, private val api: ApiClient, private val log: (String) -> Unit) {
    private val prefs = context.getSharedPreferences("phone_staged", Context.MODE_PRIVATE)
    private val busy = AtomicBoolean(false)

    /** Call from a background thread. Does nothing unless the accessibility service is on. */
    fun tick() {
        val screen = GriffinAccess.instance ?: return
        if (!busy.compareAndSet(false, true)) return
        try {
            val task = api.claimPhoneTask() ?: return
            log("phone job started")
            try {
                handle(task, screen)
            } catch (e: Exception) {
                runCatching { api.failTask(task.id, "The phone job stopped: ${e.message}", retryable = false) }
            }
        } catch (e: Exception) {
            log("phone job claim: ${e.message}")
        } finally {
            busy.set(false)
        }
    }

    private fun handle(task: ApiClient.PhoneTask, screen: GriffinAccess) {
        staged(task.id)?.let { (goal, steps, digest) ->
            if (PhoneScript.digest(goal, steps) == digest && PhoneScript.approvalType(digest) in task.approved) {
                val err = PhoneScript.replay(screen, steps)
                prefs.edit().remove(task.id).apply()
                if (err == null) api.completeTask(task.id, "Done. I carried out the steps you approved.") else api.failTask(task.id, err, retryable = false)
                return
            }
        }
        val brain = object : PBrain {
            override fun next(goal: String, screen: PScreen, done: List<PStep>): PDecision {
                runCatching { api.renewLease(task.id) }
                return api.deviceNext(goal, screen, done)
            }
        }
        when (val run = PhoneScript.run(task.goal, screen, brain)) {
            is PRun.Answer -> api.completeTask(task.id, run.result)
            is PRun.Failed -> api.failTask(task.id, run.reason, retryable = false)
            is PRun.Staged -> {
                prefs.edit().putString(task.id, JSONObject().put("goal", task.goal).put("digest", run.digest).put("steps", PhoneScript.toJson(run.steps)).toString()).apply()
                api.requestApproval(
                    task.id, PhoneScript.approvalType(run.digest), "Use the phone: ${task.goal.take(150)}. Ready to finish: ${run.preview.lines().last()}",
                    JSONObject().put("digest", run.digest).put("goal", task.goal).put("preview", run.preview).put("steps", PhoneScript.toJson(run.steps)),
                )
            }
        }
    }

    private fun staged(taskId: String): Triple<String, List<PStep>, String>? {
        val raw = prefs.getString(taskId, null) ?: return null
        return try {
            val o = JSONObject(raw)
            Triple(o.getString("goal"), PhoneScript.fromJson(o.getJSONArray("steps")), o.getString("digest"))
        } catch (_: Exception) { null }
    }
}
