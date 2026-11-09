# Source this to build the Android app without Android Studio:  source scripts/android-env.sh
# The JDK, SDK and Gradle live in ~/Android (see docs/PRD.md 7.18).
export JAVA_HOME="$HOME/Android/jdk-17"
export ANDROID_HOME="$HOME/Android/Sdk"
export ANDROID_SDK_ROOT="$ANDROID_HOME"
export PATH="$JAVA_HOME/bin:$ANDROID_HOME/platform-tools:$ANDROID_HOME/cmdline-tools/latest/bin:$HOME/Android/gradle-8.9/bin:$PATH"
