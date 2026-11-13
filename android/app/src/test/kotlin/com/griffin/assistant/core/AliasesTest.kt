package com.griffin.assistant.core

import org.junit.Test
import org.junit.Assert.assertEquals

class AliasesTest {
    private val a = mapOf("mummy" to "mom", "bhai" to "anish aggarwal", "bade bhai" to "rohan")

    @Test fun rewritesWholeWordsOnly() {
        assertEquals("call mom", Aliases.apply("call mummy", a))
        assertEquals("Call mom now", Aliases.apply("Call MUMMY now", a))
        assertEquals("mummys recipe", Aliases.apply("mummys recipe", a))
        assertEquals("mom, call anish aggarwal.", Aliases.apply("mummy, call bhai.", a))
    }

    @Test fun longestAliasWins() {
        assertEquals("open whatsapp chat with rohan", Aliases.apply("open whatsapp chat with bade bhai", a))
    }

    @Test fun nothingToDoLeavesTextAlone() {
        assertEquals("hi", Aliases.apply("hi", emptyMap()))
    }
}
