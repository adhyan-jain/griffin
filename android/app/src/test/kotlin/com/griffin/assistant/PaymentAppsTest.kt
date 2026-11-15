package com.griffin.assistant

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class PaymentAppsTest {

    @Test
    fun `identifies known Indian UPI and banking apps`() {
        assertTrue(PaymentApps.isPaymentApp(null, "in.org.npci.upiapp"))
        assertTrue(PaymentApps.isPaymentApp(null, "com.google.android.apps.nbu.paisa.user"))
        assertTrue(PaymentApps.isPaymentApp(null, "com.phonepe.app"))
        assertTrue(PaymentApps.isPaymentApp(null, "net.one97.paytm"))
        assertTrue(PaymentApps.isPaymentApp(null, "com.canarabank.mobility"))
        assertTrue(PaymentApps.isPaymentApp(null, "com.dreamplug.androidapp"))
        assertTrue(PaymentApps.isPaymentApp(null, "com.sbi.lotusintouch"))
        assertTrue(PaymentApps.isPaymentApp(null, "com.snapwork.hdfc"))
    }

    @Test
    fun `identifies generic banking and fintech by keywords`() {
        assertTrue(PaymentApps.isPaymentApp(null, "com.example.mybank"))
        assertTrue(PaymentApps.isPaymentApp(null, "org.custom.upiapplication"))
        assertTrue(PaymentApps.isPaymentApp(null, "com.cool.fintech.wallet"))
    }

    @Test
    fun `ignores normal apps, browsers, launchers and griffin`() {
        assertFalse(PaymentApps.isPaymentApp(null, "com.brave.browser"))
        assertFalse(PaymentApps.isPaymentApp(null, "com.android.chrome"))
        assertFalse(PaymentApps.isPaymentApp(null, "com.google.android.gm"))
        assertFalse(PaymentApps.isPaymentApp(null, "com.whatsapp"))
        assertFalse(PaymentApps.isPaymentApp(null, "com.android.launcher"))
        assertFalse(PaymentApps.isPaymentApp(null, "com.griffin.assistant"))
        assertFalse(PaymentApps.isPaymentApp(null, "com.android.systemui"))
        assertFalse(PaymentApps.isPaymentApp(null, "com.android.credentialmanager"))
        assertFalse(PaymentApps.isPaymentApp(null, "com.google.android.permissioncontroller"))
    }

    @Test
    fun `identifies transient system packages`() {
        assertTrue(PaymentApps.isTransientSystemPackage("android"))
        assertTrue(PaymentApps.isTransientSystemPackage("com.android.systemui"))
        assertTrue(PaymentApps.isTransientSystemPackage("com.google.android.inputmethod.latin"))
        assertTrue(PaymentApps.isTransientSystemPackage("com.android.credentialmanager"))
        assertTrue(PaymentApps.isTransientSystemPackage("com.google.android.permissioncontroller"))
        assertFalse(PaymentApps.isTransientSystemPackage("com.brave.browser"))
        assertFalse(PaymentApps.isTransientSystemPackage("com.android.launcher"))
    }
}
