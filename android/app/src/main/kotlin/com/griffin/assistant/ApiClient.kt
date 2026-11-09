package com.griffin.assistant

import org.json.JSONArray
import org.json.JSONObject
import java.io.BufferedReader
import java.net.HttpURLConnection
import java.net.URL
import java.util.UUID

/** What the voice route answered. */
data class VoiceReply(val kind: String, val reply: String, val handoff: Boolean, val managerName: String, val fallback: String?)

class ApiException(message: String, val status: Int = 0) : Exception(message)

/** Parses the voice route's reply (a pure function, so it is tested without a phone). */
fun parseVoiceReply(body: String): VoiceReply {
    val root = JSONObject(body)
    val d = root.optJSONObject("data") ?: root
    return VoiceReply(d.optString("kind", "conversation"), d.optString("reply", ""), d.optBoolean("handoff", false), d.optString("manager_name", "JJ"), d.optString("fallback").ifEmpty { null })
}

/** Talks to Griffin's API with the owner's own sign-in. All calls block: run them off the main thread. */
class ApiClient(private val store: SecretStore, private val prefs: android.content.SharedPreferences) {
    var apiUrl: String
        get() = prefs.getString("api_url", null)?.takeIf { it.isNotBlank() } ?: BuildConfig.API_URL
        set(v) = prefs.edit().putString("api_url", v.trim().trimEnd('/')).apply()
    var authUrl: String
        get() = prefs.getString("auth_url", null)?.takeIf { it.isNotBlank() } ?: BuildConfig.SUPABASE_URL
        set(v) = prefs.edit().putString("auth_url", v.trim().trimEnd('/')).apply()

    val signedIn: Boolean get() = store.get("refresh") != null
    var email: String
        get() = prefs.getString("email", "") ?: ""
        private set(v) = prefs.edit().putString("email", v).apply()

    private fun call(method: String, url: String, headers: Map<String, String>, body: String?): Pair<Int, String> {
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
        store.put("access", null)
        store.put("refresh", null)
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

    fun managerName(): String = JSONObject(api("GET", "/api/v1/settings/manager-name")).let { (it.optJSONObject("data") ?: it).optString("manager_name", "JJ") }

    private fun historyJson(history: List<Pair<String, String>>) = JSONArray().apply { history.forEach { put(JSONObject().put("role", it.first).put("text", it.second)) } }

    /** A quick spoken answer, or a hand-off when the request is real work. */
    fun voiceTurn(text: String, history: List<Pair<String, String>>): VoiceReply =
        parseVoiceReply(api("POST", "/api/v1/voice/turn", JSONObject().put("text", text).put("history", historyJson(history))))

    /** Sends real work to the manager with the conversation so far. */
    fun sendMessage(text: String, history: List<Pair<String, String>>) {
        api("POST", "/api/v1/jj/messages", JSONObject().put("message", text).put("history", historyJson(history)).put("idempotency_key", UUID.randomUUID().toString()))
    }
}
