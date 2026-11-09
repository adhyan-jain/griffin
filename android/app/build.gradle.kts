import java.util.Properties

plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
}

// Public settings (the API address and the sign-in project's public key) come from local.properties, which is
// not committed. Nothing secret is baked into the app: the owner signs in with their own account.
val local = Properties().apply { rootProject.file("local.properties").takeIf { it.exists() }?.inputStream()?.use { load(it) } }
fun prop(name: String) = (local.getProperty(name) ?: "").replace("\"", "")

android {
    namespace = "com.griffin.assistant"
    compileSdk = 35

    defaultConfig {
        applicationId = "com.griffin.assistant"
        minSdk = 29 // Android 10: the calling and background-service APIs this app needs
        targetSdk = 35
        versionCode = 1
        versionName = "0.1.0"
        buildConfigField("String", "API_URL", "\"${prop("griffin.apiUrl")}\"")
        buildConfigField("String", "SUPABASE_URL", "\"${prop("griffin.supabaseUrl")}\"")
        buildConfigField("String", "SUPABASE_KEY", "\"${prop("griffin.supabaseAnonKey")}\"")
    }
    buildFeatures {
        buildConfig = true
    }

    buildTypes {
        release {
            isMinifyEnabled = false
        }
    }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
    kotlinOptions {
        jvmTarget = "17"
    }
}

dependencies {
    implementation("androidx.core:core-ktx:1.13.1")
    testImplementation("junit:junit:4.13.2")
    testImplementation("org.json:json:20240303")
}
