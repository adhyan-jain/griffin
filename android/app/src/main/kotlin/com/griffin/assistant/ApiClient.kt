package com.griffin.assistant

import org.json.JSONArray
import org.json.JSONObject
import java.io.BufferedReader
import java.net.HttpURLConnection
import java.net.URL
import java.util.UUID

/** What the voice route answered. */
data class VoiceReply(val kind: String, val reply: String, val handoff: Boolean, val managerName: String, val fallback: String?, val confirmToken: String? = null, val draft: String? = null)

class ApiException(message: String, val status: Int = 0) : Exception(message)

/** Parses the voice route's reply (a pure function, so it is tested without a phone). */
fun parseVoiceReply(body: String): VoiceReply {
    val root = JSONObject(body)
    val d = root.optJSONObject("data") ?: root
    return VoiceReply(d.optString("kind", "conversation"), d.optString("reply", ""), d.optBoolean("handoff", false), d.optString("manager_name", "JJ"), d.optString("fallback").ifEmpty { null }, d.optString("confirm_token").ifEmpty { null }, d.optString("draft").ifEmpty { null })
}

/** What the phone can do for Griffin today. Advertised on every heartbeat; the owner can still narrow it on the web. */
val PHONE_CAPABILITIES = listOf("phone_apps", "sms", "calls", "messaging")
/** Granted at registration; advertised on a heartbeat only while the accessibility service is actually on. */
const val PHONE_UI = "phone_ui"

/** Talks to Griffin's API with the owner's own sign-in. All calls block: run them off the main thread. */
class ApiClient(private val store: SecretStore, private val prefs: android.content.SharedPreferences) {
    // A different server (a test stand-in) is honoured only in debug builds, and signing out puts the real one back.
    // A release build ignores any saved address, so a stale test setting can never leave it talking to nothing.
    var apiUrl: String
        get() = (if (BuildConfig.DEBUG) prefs.getString("api_url", null)?.takeIf { it.isNotBlank() } else null) ?: BuildConfig.API_URL
        set(v) = prefs.edit().putString("api_url", v.trim().trimEnd('/')).apply()
    var authUrl: String
        get() = (if (BuildConfig.DEBUG) prefs.getString("auth_url", null)?.takeIf { it.isNotBlank() } else null) ?: BuildConfig.SUPABASE_URL
        set(v) = prefs.edit().putString("auth_url", v.trim().trimEnd('/')).apply()

    /** True while the app is pointed at something other than the real server (debug builds only). */
    val usingTestServer: Boolean get() = apiUrl != BuildConfig.API_URL || authUrl != BuildConfig.SUPABASE_URL

    val signedIn: Boolean get() = store.get("refresh") != null
    var email: String
        get() = prefs.getString("email", "") ?: ""
        private set(v) = prefs.edit().putString("email", v).apply()

    private fun call(method: String, url: String, headers: Map<String, String>, body: String?): Pair<Int, String> =
        try {
            callOnce(method, url, headers, body)
        } catch (e: java.io.IOException) {
            // Say where it could not connect and why, in words (never the request, which may hold a password).
            val host = runCatching { URL(url).let { "${it.host}${if (it.port > 0) ":${it.port}" else ""}" } }.getOrDefault("the server")
            throw ApiException("could not connect to $host (${e.javaClass.simpleName.replace("Exception", "").lowercase().ifEmpty { "network" }})", 0)
        }

    private fun callOnce(method: String, url: String, headers: Map<String, String>, body: String?): Pair<Int, String> {
        val c = (URL(url).openConnection() as HttpURLConnection).apply {
            requestMethod = method
            connectTimeout = 8000
            readTimeout = 20000
            headers.forEach { (k, v) -> setRequestProperty(k, v) }
            if (body != null) {
                doOutput = true
                setRequestProperty("Content-Type", "application/json")
                outputStream.use { it.write(body.toByteArray(Charsets.UTF_8)) }
            }
        }
        val code = c.responseCode
        val stream = if (code in 200..299) c.inputStream else c.errorStream
        val text = stream?.let { BufferedReader(it.reader()).use { r -> r.readText() } } ?: ""
        return code to text
    }

    private fun message(text: String, fallback: String): String =
        try {
            val j = JSONObject(text)
            j.optJSONObject("error")?.optString("message")?.ifEmpty { null } ?: j.optString("error_description").ifEmpty { null } ?: j.optString("msg").ifEmpty { null } ?: fallback
        } catch (_: Exception) {
            fallback
        }

    private fun saveSession(text: String) {
        val j = JSONObject(text)
        store.put("access", j.getString("access_token"))
        store.put("refresh", j.getString("refresh_token"))
        prefs.edit().putLong("expires_at", System.currentTimeMillis() + j.optLong("expires_in", 3600) * 1000).apply()
    }

    /** Signs in with the same email and password as the web app. The password is sent once and not kept. */
    fun signIn(email: String, password: String) {
        val (code, text) = call("POST", "$authUrl/auth/v1/token?grant_type=password", mapOf("apikey" to BuildConfig.SUPABASE_KEY), JSONObject().put("email", email).put("password", password).toString())
        if (code != 200) throw ApiException(message(text, "Sign-in failed (HTTP $code)."), code)
        saveSession(text)
        this.email = email
    }

    fun signOut() {
        store.put("host_token", null)
        store.put("access", null)
        store.put("refresh", null)
        prefs.edit().remove("api_url").remove("auth_url").remove("email").remove("manager_name").remove("expires_at").apply()
    }

    @Synchronized
    private fun token(): String {
        val access = store.get("access")
        if (access != null && System.currentTimeMillis() < prefs.getLong("expires_at", 0) - 60_000) return access
        val refresh = store.get("refresh") ?: throw ApiException("Not signed in.", 401)
        val (code, text) = call("POST", "$authUrl/auth/v1/token?grant_type=refresh_token", mapOf("apikey" to BuildConfig.SUPABASE_KEY), JSONObject().put("refresh_token", refresh).toString())
        if (code != 200) {
            if (code in 400..401) signOut()
            throw ApiException("Your sign-in has expired. Sign in again in the Griffin app.", code)
        }
        saveSession(text)
        return store.get("access") ?: throw ApiException("Not signed in.", 401)
    }

    private fun api(method: String, path: String, body: JSONObject? = null): String {
        val (code, text) = call(method, "$apiUrl$path", mapOf("Authorization" to "Bearer ${token()}"), body?.toString())
        if (code !in 200..299) throw ApiException(message(text, "Griffin answered HTTP $code."), code)
        return text
    }

    /**
     * Registers this phone as a Griffin device the first time, then reports that it is alive. Without this the web
     * app's Devices page never shows the phone. The host token is kept in [SecretStore] and dropped if the owner
     * revokes the device, so the next beat registers a fresh one. Blocks: call it off the main thread.
     */
    fun phoneHeartbeat(withPhoneUi: Boolean = false) {
        var hostToken = store.get("host_token")
        if (hostToken == null) {
            val body = JSONObject().put("name", "${android.os.Build.MANUFACTURER} ${android.os.Build.MODEL}".trim()).put("kind", "phone")
                .put("capabilities", JSONArray(PHONE_CAPABILITIES + PHONE_UI))
            val reply = JSONObject(api("POST", "/api/v1/hosts", body)).let { it.optJSONObject("data") ?: it }
            hostToken = reply.getString("token")
            store.put("host_token", hostToken)
        }
        val (code, text) = call("POST", "$apiUrl/api/v1/host/heartbeat", mapOf("Authorization" to "Bearer $hostToken"),
            JSONObject().put("capabilities", JSONArray(if (withPhoneUi) PHONE_CAPABILITIES + PHONE_UI else PHONE_CAPABILITIES)).put("version", BuildConfig.VERSION_NAME).toString())
        if (code == 401) store.put("host_token", null)
        if (code !in 200..299) throw ApiException(message(text, "Device heartbeat failed (HTTP $code)."), code)
    }

    /** Names of the people an outreach campaign is waiting to hear from (nothing else is ever sent about chats). */
    fun awaitingNames(): List<String> {
        val root = JSONObject(api("GET", "/api/v1/outreach/awaiting"))
        val arr = root.optJSONArray("data") ?: return emptyList()
        return (0 until arr.length()).mapNotNull { arr.optJSONObject(it)?.optString("name")?.takeIf { n -> n.isNotBlank() } }
    }

    /** Tells Griffin a person it is waiting on has answered. */
    fun reportReply(name: String, text: String) {
        api("POST", "/api/v1/outreach/replies", JSONObject().put("name", name).put("text", text.take(2000)))
    }

    // ---- the host protocol, as this phone (the host token, not the owner's sign-in) -----------------------------

    private fun host(method: String, path: String, body: JSONObject? = null): Pair<Int, String> {
        val token = store.get("host_token") ?: throw ApiException("This phone is not registered yet.", 401)
        val r = call(method, "$apiUrl$path", mapOf("Authorization" to "Bearer $token"), body?.toString() ?: if (method == "POST") "{}" else null)
        if (r.first == 401) store.put("host_token", null)
        if (r.first !in 200..299) throw ApiException(message(r.second, "Griffin answered HTTP ${r.first}."), r.first)
        return r
    }

    /** A claimed phone task: its id, the phone job's goal and the approvals the owner already granted. */
    data class PhoneTask(val id: String, val goal: String, val approved: Set<String>)

    /** Leases the next task for this phone, or null. Tasks that are not phone jobs are failed back, not run. */
    fun claimPhoneTask(): PhoneTask? {
        val (code, text) = host("POST", "/api/v1/host/claim")
        if (code == 204 || text.isBlank()) return null
        val root = JSONObject(text).let { it.optJSONObject("data") ?: it }
        val task = root.optJSONObject("task") ?: return null
        val id = task.getString("id")
        val payload = task.optJSONObject("input_payload")
        val goal = payload?.optJSONObject("phone")?.optString("goal").orEmpty()
        val approved = root.optJSONArray("approved_actions")?.let { a -> (0 until a.length()).map { a.getString(it) }.toSet() } ?: emptySet()
        if (goal.isEmpty()) { failTask(id, "This phone only runs phone jobs.", retryable = true); return null }
        return PhoneTask(id, goal, approved)
    }

    fun renewLease(taskId: String) { host("POST", "/api/v1/host/tasks/$taskId/heartbeat") }
    fun completeTask(taskId: String, response: String) { host("POST", "/api/v1/host/tasks/$taskId/complete", JSONObject().put("output", JSONObject().put("response", response))) }
    fun failTask(taskId: String, reason: String, retryable: Boolean) { host("POST", "/api/v1/host/tasks/$taskId/fail", JSONObject().put("reason", reason.take(500)).put("retryable", retryable)) }
    fun requestApproval(taskId: String, actionType: String, summary: String, payload: JSONObject) {
        host("POST", "/api/v1/host/tasks/$taskId/approval", JSONObject().put("action_type", actionType).put("summary", summary.take(500)).put("risk_level", "HIGH").put("payload", payload))
    }

    /** Asks the server (which holds the model key) what to do next on this screen. */
    fun deviceNext(goal: String, screen: com.griffin.assistant.core.PScreen, done: List<com.griffin.assistant.core.PStep>): com.griffin.assistant.core.PDecision {
        val page = JSONObject().put("url", screen.app).put("title", screen.title).put("text", screen.text.take(4000))
            .put("elements", JSONArray().apply { screen.elements.forEach { put(JSONObject().put("ref", it.ref).put("role", it.role).put("name", it.name).put("value", it.value)) } })
        val (_, text) = host("POST", "/api/v1/host/device/next", JSONObject().put("goal", goal).put("page", page).put("done", com.griffin.assistant.core.PhoneScript.toJson(done)))
        val d = JSONObject(text).let { it.optJSONObject("data") ?: it }
        val step = d.optJSONObject("step")
        return com.griffin.assistant.core.PDecision(
            d.optBoolean("done"), d.optString("result"),
            com.griffin.assistant.core.PStep(step?.optString("kind").orEmpty(), step?.optString("ref").orEmpty(), "", "", step?.optString("text").orEmpty(), step?.optString("url").orEmpty(), step?.optString("note").orEmpty(), step?.optBoolean("final") ?: false),
        )
    }

    fun managerName(): String = JSONObject(api("GET", "/api/v1/settings/manager-name")).let { (it.optJSONObject("data") ?: it).optString("manager_name", "JJ") }
        .also { prefs.edit().putString("manager_name", it).apply() }

    /** The last name seen, for showing the wake phrase without a network call. */
    fun cachedName(): String = prefs.getString("manager_name", "JJ") ?: "JJ"

    private fun historyJson(history: List<Pair<String, String>>) = JSONArray().apply { history.forEach { put(JSONObject().put("role", it.first).put("text", it.second)) } }

    /** A quick spoken answer, or a hand-off when the request is real work. */
    fun voiceTurn(text: String, history: List<Pair<String, String>>, unlocked: Boolean = false): VoiceReply =
        parseVoiceReply(api("POST", "/api/v1/voice/turn", JSONObject().put("text", text).put("history", historyJson(history)).put("unlocked", unlocked)))

    /** Answers a "say yes to confirm" from Griffin (cancel, delete, approve ...) with the owner's yes or no. */
    fun voiceConfirm(token: String, yes: Boolean, unlocked: Boolean): VoiceReply =
        parseVoiceReply(api("POST", "/api/v1/voice/turn", JSONObject().put("unlocked", unlocked).put("confirm", JSONObject().put("token", token).put("yes", yes))))

    /** The owner's aliases, fetched from the server and kept so the phone understands them without a round trip. */
    fun refreshAliases() {
        val data = JSONObject(api("GET", "/api/v1/aliases", null)).let { it.optJSONObject("data") ?: it }
        val m = data.optJSONObject("aliases") ?: JSONObject()
        prefs.edit().putString("aliases", m.toString()).apply()
    }

    fun aliases(): Map<String, String> = try {
        val m = JSONObject(prefs.getString("aliases", "{}") ?: "{}")
        m.keys().asSequence().associateWith { m.getString(it) }
    } catch (_: Exception) { emptyMap() }

    /** Sends real work to the manager with the conversation so far. */
    fun sendMessage(text: String, history: List<Pair<String, String>>) {
        api("POST", "/api/v1/jj/messages", JSONObject().put("message", text).put("history", historyJson(history)).put("idempotency_key", UUID.randomUUID().toString()))
    }
}
