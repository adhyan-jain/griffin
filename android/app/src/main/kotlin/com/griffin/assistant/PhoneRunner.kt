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
class PhoneRunner(
    private val context: Context,
    private val api: ApiClient,
    private val cellularCallManager: CellularCallManager? = null,
    private val log: (String) -> Unit,
) {
    private val prefs = context.getSharedPreferences("phone_staged", Context.MODE_PRIVATE)
    private val busy = AtomicBoolean(false)

    /** Call from a background thread. Runs phone automation tasks and automated cellular calls. */
    fun tick() {
        if (!busy.compareAndSet(false, true)) return
        try {
            val task = api.claimPhoneTask() ?: return
            log("phone job started: ${task.id}")
            task.control?.let { c ->
                val r = runPhoneControl(c, ActionsPhoneControl(Actions(context)))
                if (r.ok) api.completeTask(task.id, r.text) else api.failTask(task.id, r.text, retryable = false)
                return
            }
            try {
                // Wake display and request keyguard dismissal so UI accessibility and dialer are available
                try {
                    val pm = context.getSystemService(Context.POWER_SERVICE) as android.os.PowerManager
                    if (!pm.isInteractive) {
                        @Suppress("DEPRECATION")
                        val wakeLock = pm.newWakeLock(
                            android.os.PowerManager.FULL_WAKE_LOCK or android.os.PowerManager.ACQUIRE_CAUSES_WAKEUP or android.os.PowerManager.ON_AFTER_RELEASE,
                            "griffin:phone_task_wake"
                        )
                        wakeLock.acquire(8000L)
                    }
                    GriffinAccess.instance?.performGlobalAction(6 /* GLOBAL_ACTION_DISMISS_KEYGUARD */)
                } catch (_: Exception) {}

                if (task.action in listOf("automated_call", "call", "direct_call", "whatsapp_call", "send_sms", "sms")) {
                    handleCall(task)
                } else {
                    val screen = GriffinAccess.instance
                    if (screen == null) {
                        api.failTask(task.id, "Accessibility service is off.", retryable = true)
                        return
                    }
                    handle(task, screen)
                }
            } catch (e: Exception) {
                runCatching { api.failTask(task.id, "The phone job stopped: ${e.message}", retryable = false) }
            }
        } catch (e: Exception) {
            log("phone job claim: ${e.message}")
        } finally {
            busy.set(false)
        }
    }

    private fun handleCall(task: ApiClient.PhoneTask) {
        when {
            task.channel == "whatsapp" || task.action == "whatsapp_call" -> handleWhatsAppCall(task)
            task.action in listOf("send_sms", "sms") -> handleSms(task)
            task.callMode == "direct_talk" || task.action == "direct_call" -> handleDirectCall(task)
            else -> handleAutomatedCall(task)
        }
    }

    private fun handleWhatsAppCall(task: ApiClient.PhoneTask) {
        val query = task.recipientName.ifEmpty { task.number }.trim()
        val match: Contacts.Match = when (val res = Contacts.resolveWhatsApp(context, query)) {
            is Contacts.Resolution.Resolved -> res.match
            is Contacts.Resolution.Ambiguous -> {
                val candidates = res.candidates.take(5).joinToString(", ") { "${it.name} (${it.number.takeLast(4).padStart(it.number.length, '*')})" }
                api.failTask(task.id, "Ambiguous contact name '$query'. Multiple matches found: $candidates. Please clarify which person you mean.", retryable = false)
                return
            }
            is Contacts.Resolution.NotFound -> {
                api.failTask(task.id, "Contact not found for: $query", retryable = false)
                return
            }
            is Contacts.Resolution.PermissionDenied -> {
                api.failTask(task.id, "Contact permission denied. Please grant Contacts permission to Griffin.", retryable = false)
                return
            }
        }

        val targetName = match.name.ifEmpty { query }
        val targetNumber = match.number
        val approvalKey = "host_action:call:${targetName.lowercase()}"
        val numApprovalKey = "host_action:call:${targetNumber.lowercase()}"
        val waApprovalKey = "host_action:whatsapp_call:${targetName.lowercase()}"
        if (approvalKey !in task.approved && numApprovalKey !in task.approved && waApprovalKey !in task.approved && "host_action:call:*" !in task.approved) {
            val payload = JSONObject()
                .put("action", "whatsapp_call")
                .put("target_name", targetName)
                .put("target_number", targetNumber)
                .put("whatsapp_data_id", match.whatsAppDataId)
            api.requestApproval(
                task.id,
                approvalKey,
                "Place WhatsApp call to $targetName ($targetNumber)",
                payload
            )
            return
        }

        val res = Actions(context).callWhatsAppWithResult(match)
        when (res) {
            is Actions.ActionResult.Started -> api.completeTask(task.id, res.message)
            is Actions.ActionResult.Completed -> api.completeTask(task.id, res.message)
            is Actions.ActionResult.Failed -> api.failTask(task.id, res.error, retryable = res.retryable)
            is Actions.ActionResult.Unknown -> api.failTask(task.id, res.reason, retryable = false)
        }
    }

    private fun handleDirectCall(task: ApiClient.PhoneTask) {
        val query = task.recipientName.ifEmpty { task.number }.trim()
        val match: Contacts.Match = if (task.number.isNotEmpty() && task.number.all { it.isDigit() || it == '+' || it == ' ' || it == '-' }) {
            Contacts.Match(name = task.recipientName.ifEmpty { task.number }, number = task.number)
        } else {
            when (val res = Contacts.resolve(context, query)) {
                is Contacts.Resolution.Resolved -> res.match
                is Contacts.Resolution.Ambiguous -> {
                    val candidates = res.candidates.take(5).joinToString(", ") { "${it.name} (${it.number.takeLast(4).padStart(it.number.length, '*')})" }
                    api.failTask(task.id, "Ambiguous contact name '$query'. Multiple matches found: $candidates. Please clarify which person you mean.", retryable = false)
                    return
                }
                is Contacts.Resolution.NotFound -> {
                    api.failTask(task.id, "Contact not found for: $query", retryable = false)
                    return
                }
                is Contacts.Resolution.PermissionDenied -> {
                    api.failTask(task.id, "Contact permission denied. Please grant Contacts permission to Griffin.", retryable = false)
                    return
                }
            }
        }

        val targetName = match.name.ifEmpty { query }
        val targetNumber = match.number
        val approvalKey = "host_action:call:${targetName.lowercase()}"
        val numApprovalKey = "host_action:call:${targetNumber.lowercase()}"
        if (approvalKey !in task.approved && numApprovalKey !in task.approved && "host_action:call:*" !in task.approved) {
            val payload = JSONObject()
                .put("action", "direct_call")
                .put("target_name", targetName)
                .put("target_number", targetNumber)
            api.requestApproval(
                task.id,
                approvalKey,
                "Place direct cellular phone call to $targetName ($targetNumber)",
                payload
            )
            return
        }

        val res = Actions(context).callWithResult(targetNumber)
        when (res) {
            is Actions.ActionResult.Started -> api.completeTask(task.id, "Placed direct call to $targetName ($targetNumber) for owner.")
            is Actions.ActionResult.Completed -> api.completeTask(task.id, res.message)
            is Actions.ActionResult.Failed -> api.failTask(task.id, res.error, retryable = res.retryable)
            is Actions.ActionResult.Unknown -> api.failTask(task.id, res.reason, retryable = false)
        }
    }

    private fun handleSms(task: ApiClient.PhoneTask) {
        val query = task.recipientName.ifEmpty { task.number }.trim()
        val match: Contacts.Match = if (task.number.isNotEmpty() && task.number.all { it.isDigit() || it == '+' || it == ' ' || it == '-' }) {
            Contacts.Match(name = task.recipientName.ifEmpty { task.number }, number = task.number)
        } else {
            when (val res = Contacts.resolve(context, query)) {
                is Contacts.Resolution.Resolved -> res.match
                is Contacts.Resolution.Ambiguous -> {
                    val candidates = res.candidates.take(5).joinToString(", ") { "${it.name} (${it.number.takeLast(4).padStart(it.number.length, '*')})" }
                    api.failTask(task.id, "Ambiguous contact name '$query'. Multiple matches found: $candidates. Please clarify which person you mean.", retryable = false)
                    return
                }
                is Contacts.Resolution.NotFound -> {
                    api.failTask(task.id, "Contact not found for: $query", retryable = false)
                    return
                }
                is Contacts.Resolution.PermissionDenied -> {
                    api.failTask(task.id, "Contact permission denied. Please grant Contacts permission to Griffin.", retryable = false)
                    return
                }
            }
        }

        val targetName = match.name.ifEmpty { query }
        val targetNumber = match.number
        val approvalKey = "host_action:sms:${targetName.lowercase()}"
        val numApprovalKey = "host_action:sms:${targetNumber.lowercase()}"
        if (approvalKey !in task.approved && numApprovalKey !in task.approved && "host_action:sms:*" !in task.approved) {
            val payload = JSONObject()
                .put("action", "send_sms")
                .put("target_name", targetName)
                .put("target_number", targetNumber)
                .put("message", task.message)
            api.requestApproval(
                task.id,
                approvalKey,
                "Send SMS to $targetName ($targetNumber): \"${task.message}\"",
                payload
            )
            return
        }

        val res = Actions(context).sendSmsWithResult(targetNumber, task.message)
        when (res) {
            is Actions.ActionResult.Started -> api.completeTask(task.id, res.message)
            is Actions.ActionResult.Completed -> api.completeTask(task.id, res.message)
            is Actions.ActionResult.Failed -> api.failTask(task.id, res.error, retryable = res.retryable)
            is Actions.ActionResult.Unknown -> api.failTask(task.id, res.reason, retryable = false)
        }
    }

    private fun handleAutomatedCall(task: ApiClient.PhoneTask) {
        val manager = cellularCallManager ?: run {
            api.failTask(task.id, "CellularCallManager is not available on this device.", retryable = false)
            return
        }

        var targetNumber = task.number.trim()
        var targetName = task.recipientName.trim()
        if (targetNumber.isEmpty() || !targetNumber.any { it.isDigit() }) {
            val query = targetName.ifEmpty { targetNumber }
            when (val res = Contacts.resolve(context, query)) {
                is Contacts.Resolution.Resolved -> {
                    targetNumber = res.match.number
                    if (targetName.isEmpty()) targetName = res.match.name
                }
                is Contacts.Resolution.Ambiguous -> {
                    val candidates = res.candidates.take(5).joinToString(", ") { "${it.name} (${it.number.takeLast(4).padStart(it.number.length, '*')})" }
                    api.failTask(task.id, "Ambiguous contact name '$query'. Multiple matches found: $candidates. Please clarify which person you mean.", retryable = false)
                    return
                }
                is Contacts.Resolution.NotFound -> {
                    api.failTask(task.id, "Contact not found for: $query", retryable = false)
                    return
                }
                is Contacts.Resolution.PermissionDenied -> {
                    api.failTask(task.id, "Contact permission denied. Please grant Contacts permission to Griffin.", retryable = false)
                    return
                }
            }
        }

        // Strictly verify that the owner has explicitly approved placing this call to targetName
        val approvalKey = "host_action:call:${targetName.lowercase()}"
        val numApprovalKey = "host_action:call:${targetNumber.lowercase()}"
        if (approvalKey !in task.approved && numApprovalKey !in task.approved && "host_action:call:*" !in task.approved) {
            val payload = JSONObject()
                .put("action", "automated_call")
                .put("target_name", targetName)
                .put("target_number", targetNumber)
                .put("message", task.message)
            api.requestApproval(
                task.id,
                approvalKey,
                "Call $targetName ($targetNumber) with message: \"${task.message}\"",
                payload
            )
            return
        }

        val latch = java.util.concurrent.CountDownLatch(1)
        var callOutcome = "failed"
        var callReply = ""
        var callFollowup = ""

        val started = manager.startAutomatedCall(
            number = targetNumber,
            recipientName = targetName,
            message = task.message,
            callId = task.callId,
            inBackground = task.inBackground,
            onDone = { outcome, reply, followup ->
                callOutcome = outcome
                callReply = reply
                callFollowup = followup
                latch.countDown()
            }
        )

        if (!started) {
            api.failTask(task.id, "Could not place the automated call (busy or missing permissions).", retryable = false)
            return
        }

        // Wait up to 3 minutes for the call to finish, renewing lease every 15 seconds
        val deadline = System.currentTimeMillis() + 180_000
        while (!latch.await(15, java.util.concurrent.TimeUnit.SECONDS)) {
            if (System.currentTimeMillis() > deadline) break
            runCatching { api.renewLease(task.id) }
        }

        if (callOutcome in listOf("delivered", "replied", "completed")) {
            val responseText = buildString {
                append("Call to ${task.recipientName.ifEmpty { task.number }} completed ($callOutcome).")
                if (callReply.isNotBlank()) append(" Reply: \"$callReply\"")
                if (callFollowup.isNotBlank()) append(" Notes: \"$callFollowup\"")
            }
            api.completeTask(task.id, responseText)
        } else {
            api.failTask(task.id, "Call to ${task.recipientName.ifEmpty { task.number }} ended with outcome: $callOutcome", retryable = false)
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
