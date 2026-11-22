package com.griffin.assistant

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class ApiParseTest {
    @Test fun `a conversation reply`() {
        val r = parseVoiceReply("""{"success":true,"data":{"kind":"conversation","reply":"Paris.","handoff":false,"manager_name":"JJ"}}""")
        assertEquals("conversation", r.kind); assertEquals("Paris.", r.reply); assertNull(r.confirmToken)
    }

    @Test fun `a task command that asks for a yes carries its token`() {
        val r = parseVoiceReply("""{"data":{"kind":"command","reply":"Delete 2 finished tasks? Say yes to confirm.","confirm_token":"abc.def","manager_name":"Jarvis"}}""")
        assertEquals("command", r.kind); assertEquals("abc.def", r.confirmToken); assertEquals("Jarvis", r.managerName)
    }

    @Test fun `missing fields get safe defaults`() {
        val r = parseVoiceReply("""{"data":{}}""")
        assertEquals("", r.reply); assertEquals(false, r.handoff); assertNull(r.confirmToken)
    }

    @Test fun `an outbox page from the server`() {
        val p = parseOutboxPage("""{"success":true,"data":{"cursor":7,"data":[{"id":"a","seq":7,"kind":"approval","entity_id":"x","title":"JJ: waiting for you","body":"Send it?","priority":"HIGH","action_url":"https://app/approvals?id=x"},{"id":"","seq":8}]}}""")
        assertEquals(7L, p.cursor)
        assertEquals(1, p.items.size)
        assertEquals("https://app/approvals?id=x", p.items[0].actionUrl)
        assertEquals("approval", p.items[0].kind)
    }
}
