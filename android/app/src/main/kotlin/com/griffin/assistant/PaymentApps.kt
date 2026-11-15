package com.griffin.assistant

import android.content.Context

/**
 * Detects payment, banking, UPI, and financial apps so overlays like the floating ball
 * can temporarily dismiss themselves, preventing touch filtering issues and security blocks.
 */
object PaymentApps {
    // Packages known specifically in India and globally for payments and banking
    val KNOWN_PACKAGES = setOf(
        "in.org.npci.upiapp", // BHIM UPI
        "com.google.android.apps.nbu.paisa.user", // Google Pay
        "com.phonepe.app", // PhonePe
        "net.one97.paytm", // Paytm
        "com.dreamplug.androidapp", // CRED
        "com.canarabank.mobility", // Canara Bank
        "com.sbi.lotusintouch", // YONO SBI
        "com.sbi.upi", // SBI Pay
        "com.snapwork.hdfc", // HDFC MobileBanking
        "com.hdfcbank.payzapp", // PayZapp
        "com.csam.icici.bank.imobile", // ICICI iMobile
        "com.axis.mobile", // Axis Mobile
        "com.msf.kbank.mobile", // Kotak Bank
        "com.bankofbaroda.mconnect", // bob World
        "com.pnb.pnbone", // PNB ONE
        "com.fampay.in", // FamPay
        "indwin.c3.shareapp", // slice
        "com.epifi.gordon", // Fi Money
        "com.jupiter.money", // Jupiter
        "com.navi.android", // Navi
        "com.zerodha.kite3", // Kite
        "com.nextbillion.groww", // Groww
        "com.angelbroking.odin", // Angel One
        "com.paypal.android.p2pmobile", // PayPal
        "com.squareup.cash", // Cash App
        "com.venmo", // Venmo
        "com.freecharge.android", // Freecharge
        "com.mobikwik_new", // MobiKwik
        "com.myairtelapp.airtelmoney", // Airtel Thanks
        "com.unionbank.ecommerce.mobile.android", // Union Bank
        "com.infrasoft.indianbank", // Indian Bank
    )

    private val KEYWORDS = listOf(
        "upi", "paytm", "phonepe", "paisa", "wallet", "banking", "bank",
        "canara", "hdfc", "icici", "kotak", "baroda", "fampay", "dreamplug",
        "finance", "fintech", "payment", "netbanking", "bhim", "yono",
        "supermoney", "mobikwik", "freecharge", "payzapp", "airtelmoney",
        "spay", "samsungpay"
    )

    /**
     * Checks if the package represents a payment, banking, or financial application.
     */
    fun isPaymentApp(context: Context?, packageName: String?): Boolean {
        if (packageName.isNullOrBlank()) return false
        val pkg = packageName.lowercase()

        // Exclude system UI, launchers, Griffin itself, credential managers, app stores
        if (pkg.contains("griffin") || pkg == "android" || pkg.contains("systemui") ||
            pkg.contains("launcher") || pkg.contains("inputmethod") || pkg.contains("credential") ||
            pkg.contains("permissioncontroller") || pkg.contains("vending")) {
            return false
        }

        if (pkg in KNOWN_PACKAGES) return true

        for (kw in KEYWORDS) {
            if (pkg.contains(kw)) return true
        }

        if (context != null) {
            try {
                val pm = context.packageManager
                val info = pm.getApplicationInfo(packageName, 0)
                val label = pm.getApplicationLabel(info).toString().lowercase()
                for (kw in KEYWORDS) {
                    if (label.contains(kw)) return true
                }
            } catch (_: Exception) {}
        }

        return false
    }

    /**
     * Checks if a package is a transient system overlay or dialog (e.g. systemui, keyboard, permissions)
     * which shouldn't cause us to leave the payment-paused state if we're currently on a payment app.
     */
    fun isTransientSystemPackage(packageName: String?): Boolean {
        if (packageName.isNullOrBlank()) return true
        val pkg = packageName.lowercase()
        return pkg == "android" ||
            pkg.contains("systemui") ||
            pkg.contains("inputmethod") ||
            pkg.contains("keyguard") ||
            pkg.contains("permissioncontroller") ||
            pkg.contains("credentialmanager") ||
            pkg.endsWith(".resolver")
    }
}
