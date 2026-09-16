package dev.eswink.aicontrolhud;

import android.content.Context;
import android.speech.tts.TextToSpeech;

import java.util.ArrayDeque;
import java.util.Locale;

final class VoiceNotifier {
    private static final int MAX_PENDING_UTTERANCES = 8;

    private final ArrayDeque<String> pending = new ArrayDeque<>();
    private final Runnable statusChanged;
    private TextToSpeech textToSpeech;
    private boolean ready;
    private long utteranceSequence;

    VoiceNotifier(Context context, Runnable statusChanged) {
        this.statusChanged = statusChanged;
        textToSpeech = new TextToSpeech(context.getApplicationContext(), this::onInit);
    }

    boolean isReady() {
        return ready;
    }

    void speak(String text) {
        if (text == null || text.trim().isEmpty()) return;
        String normalized = text.trim();
        if (!ready || textToSpeech == null) {
            while (pending.size() >= MAX_PENDING_UTTERANCES) pending.removeFirst();
            pending.addLast(normalized);
            return;
        }
        speakNow(normalized);
    }

    void shutdown() {
        ready = false;
        pending.clear();
        if (textToSpeech != null) {
            textToSpeech.stop();
            textToSpeech.shutdown();
            textToSpeech = null;
        }
    }

    private void onInit(int status) {
        TextToSpeech tts = textToSpeech;
        if (tts == null || status != TextToSpeech.SUCCESS) {
            ready = false;
            pending.clear();
            notifyStatusChanged();
            return;
        }

        int languageResult = tts.setLanguage(Locale.getDefault());
        if (languageResult == TextToSpeech.LANG_MISSING_DATA
                || languageResult == TextToSpeech.LANG_NOT_SUPPORTED) {
            languageResult = tts.setLanguage(Locale.US);
        }
        ready = languageResult != TextToSpeech.LANG_MISSING_DATA
                && languageResult != TextToSpeech.LANG_NOT_SUPPORTED;

        if (ready) {
            while (!pending.isEmpty()) speakNow(pending.removeFirst());
        } else {
            pending.clear();
        }
        notifyStatusChanged();
    }

    private void speakNow(String text) {
        TextToSpeech tts = textToSpeech;
        if (tts == null || !ready) return;
        utteranceSequence++;
        tts.speak(
                text,
                TextToSpeech.QUEUE_ADD,
                null,
                "ai-control-hud-" + utteranceSequence
        );
    }

    private void notifyStatusChanged() {
        if (statusChanged != null) statusChanged.run();
    }
}
