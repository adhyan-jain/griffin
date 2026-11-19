package com.griffin.assistant

import android.content.Context
import android.net.Uri
import android.provider.ContactsContract

object Contacts {
    data class Match(
        val name: String,
        val number: String,
        val id: String = "",
        val whatsAppDataId: Long? = null
    )

    sealed class Resolution {
        data class Resolved(val match: Match) : Resolution()
        data class Ambiguous(val query: String, val candidates: List<Match>) : Resolution()
        data class NotFound(val query: String) : Resolution()
        data class PermissionDenied(val permission: String) : Resolution()
    }

    /**
     * Resolves candidate matches strictly without guessing.
     * Pure function for deterministic testing.
     */
    fun resolveMatches(query: String, matches: List<Match>): Resolution {
        val q = query.trim()
        if (q.isEmpty()) return Resolution.NotFound(query)
        if (matches.isEmpty()) return Resolution.NotFound(q)

        // 1. Exact full-name matches (case-insensitive)
        val exact = matches.filter { it.name.equals(q, ignoreCase = true) }
        if (exact.isNotEmpty()) {
            val uniqueNumbers = exact.map { it.number.filter { c -> c.isDigit() }.takeLast(10) }.distinct()
            if (uniqueNumbers.size == 1 && exact.size == 1) {
                return Resolution.Resolved(exact[0])
            }
            if (uniqueNumbers.size > 1) {
                // Same name but different numbers -> ambiguous, owner must clarify
                return Resolution.Ambiguous(q, exact)
            }
            // Exact same name and same phone number (duplicate cards)
            return Resolution.Resolved(exact[0])
        }

        // 2. Partial matches
        val uniqueNumbers = matches.map { it.number.filter { c -> c.isDigit() }.takeLast(10) }.distinct()
        if (matches.size == 1) {
            return Resolution.Resolved(matches[0])
        }
        if (uniqueNumbers.size == 1) {
            return Resolution.Resolved(matches[0])
        }

        return Resolution.Ambiguous(q, matches)
    }

    /**
     * Resolves a contact query strictly against Android Contacts database.
     */
    fun resolve(context: Context, query: String): Resolution {
        val q = query.trim()
        if (q.isEmpty()) return Resolution.NotFound(query)

        val matches: List<Match>
        try {
            matches = find(context, q)
        } catch (_: SecurityException) {
            return Resolution.PermissionDenied("android.permission.READ_CONTACTS")
        }

        return resolveMatches(q, matches)
    }

    /**
     * Resolves a contact specifically for WhatsApp, checking WhatsApp-specific VOIP call data rows first,
     * then falling back to regular contact resolution. Refuses ambiguity.
     */
    fun resolveWhatsApp(context: Context, query: String): Resolution {
        val q = query.trim()
        if (q.isEmpty()) return Resolution.NotFound(query)

        val candidates: List<Match>
        try {
            candidates = findWhatsAppCandidates(context, q)
        } catch (_: SecurityException) {
            return Resolution.PermissionDenied("android.permission.READ_CONTACTS")
        }

        if (candidates.isNotEmpty()) {
            val res = resolveMatches(q, candidates)
            if (res !is Resolution.NotFound) {
                return res
            }
        }

        // Fall back to general contacts database resolution
        return resolve(context, q)
    }

    /** Who a phone number belongs to, or null when it is not in the contacts. */
    fun nameForNumber(context: Context, number: String): String? = try {
        val uri = Uri.withAppendedPath(ContactsContract.PhoneLookup.CONTENT_FILTER_URI, Uri.encode(number))
        context.contentResolver.query(uri, arrayOf(ContactsContract.PhoneLookup.DISPLAY_NAME), null, null, null)?.use { if (it.moveToFirst()) it.getString(0) else null }
    } catch (_: SecurityException) {
        null
    }

    /** Contacts whose name matches what was said, exact matches first. Supports multi-part Indian names. */
    fun find(context: Context, query: String): List<Match> {
        val q = query.trim()
        if (q.isEmpty()) return emptyList()
        val out = LinkedHashMap<String, Match>()

        fun queryDb(pattern: String) {
            try {
                context.contentResolver.query(
                    ContactsContract.CommonDataKinds.Phone.CONTENT_URI,
                    arrayOf(
                        ContactsContract.CommonDataKinds.Phone.CONTACT_ID,
                        ContactsContract.CommonDataKinds.Phone.DISPLAY_NAME,
                        ContactsContract.CommonDataKinds.Phone.NUMBER
                    ),
                    "${ContactsContract.CommonDataKinds.Phone.DISPLAY_NAME} LIKE ?", arrayOf("%$pattern%"), null,
                )?.use {
                    val idIdx = it.getColumnIndex(ContactsContract.CommonDataKinds.Phone.CONTACT_ID)
                    val nameIdx = it.getColumnIndex(ContactsContract.CommonDataKinds.Phone.DISPLAY_NAME)
                    val numIdx = it.getColumnIndex(ContactsContract.CommonDataKinds.Phone.NUMBER)

                    while (it.moveToNext()) {
                        val contactId = if (idIdx >= 0) it.getString(idIdx) ?: "" else ""
                        val name = if (nameIdx >= 0) it.getString(nameIdx) ?: continue else continue
                        val number = if (numIdx >= 0) it.getString(numIdx) ?: continue else continue
                        val m = Match(name = name, number = number, id = contactId)
                        val key = "${m.name.lowercase()}::${m.number.filter { c -> c.isDigit() }.takeLast(10)}"
                        out.putIfAbsent(key, m)
                    }
                }
            } catch (e: SecurityException) {
                throw e
            } catch (_: Exception) {}
        }

        queryDb(q)
        if (out.isEmpty() && q.contains(" ")) {
            for (token in q.split(Regex("\\s+"))) {
                val t = token.trim()
                if (t.length >= 3) queryDb(t)
            }
        }

        val exact = out.values.filter { it.name.equals(q, ignoreCase = true) }
        return exact.ifEmpty { out.values.sortedBy { it.name.length } }
    }

    /**
     * Finds ALL WhatsApp voice call Data candidates for a contact query.
     * Collects all matching rows to prevent arbitrary first-row matching.
     */
    fun findWhatsAppCandidates(context: Context, query: String): List<Match> {
        val q = query.trim()
        if (q.isEmpty()) return emptyList()
        val out = LinkedHashMap<String, Match>()

        fun queryData(pattern: String) {
            try {
                val projection = arrayOf(
                    ContactsContract.Data._ID,
                    ContactsContract.Data.CONTACT_ID,
                    ContactsContract.Data.DISPLAY_NAME,
                    ContactsContract.Data.DATA1
                )
                val selection = "${ContactsContract.Data.MIMETYPE} = ? AND ${ContactsContract.Data.DISPLAY_NAME} LIKE ?"
                val selectionArgs = arrayOf("vnd.android.cursor.item/vnd.com.whatsapp.voip.call", "%$pattern%")
                context.contentResolver.query(
                    ContactsContract.Data.CONTENT_URI,
                    projection,
                    selection,
                    selectionArgs,
                    null
                )?.use { cursor ->
                    val idIdx = cursor.getColumnIndex(ContactsContract.Data._ID)
                    val contactIdIdx = cursor.getColumnIndex(ContactsContract.Data.CONTACT_ID)
                    val nameIdx = cursor.getColumnIndex(ContactsContract.Data.DISPLAY_NAME)
                    val dataIdx = cursor.getColumnIndex(ContactsContract.Data.DATA1)

                    while (cursor.moveToNext()) {
                        val dataId = if (idIdx >= 0) cursor.getLong(idIdx) else null
                        val contactId = if (contactIdIdx >= 0) cursor.getString(contactIdIdx) ?: "" else ""
                        val name = if (nameIdx >= 0) cursor.getString(nameIdx) ?: continue else continue
                        val rawNumber = if (dataIdx >= 0) cursor.getString(dataIdx) ?: "" else ""
                        val key = "${name.lowercase()}::$dataId::$rawNumber"
                        out.putIfAbsent(
                            key,
                            Match(name = name, number = rawNumber, id = contactId, whatsAppDataId = dataId)
                        )
                    }
                }
            } catch (e: SecurityException) {
                throw e
            } catch (_: Exception) {}
        }

        queryData(q)
        if (out.isEmpty() && q.contains(" ")) {
            for (token in q.split(Regex("\\s+"))) {
                val t = token.trim()
                if (t.length >= 3) queryData(t)
            }
        }

        val exact = out.values.filter { it.name.equals(q, ignoreCase = true) }
        return exact.ifEmpty { out.values.sortedBy { it.name.length } }
    }

    /** Finds the WhatsApp voice call Data ID for a contact name, or returns null. */
    fun findWhatsAppCallDataId(context: Context, query: String): Long? {
        val resolution = resolveWhatsApp(context, query)
        return if (resolution is Resolution.Resolved) resolution.match.whatsAppDataId else null
    }
}
