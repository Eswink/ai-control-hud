package dev.eswink.aicontrolhud;

import android.content.Context;
import android.util.AttributeSet;
import android.widget.TextView;

public final class ServerAddressView extends TextView {
    public ServerAddressView(Context context) {
        super(context);
    }

    public ServerAddressView(Context context, AttributeSet attrs) {
        super(context, attrs);
    }

    public ServerAddressView(Context context, AttributeSet attrs, int defStyleAttr) {
        super(context, attrs, defStyleAttr);
    }

    @Override
    public void setText(CharSequence text, BufferType type) {
        String raw = text == null ? null : text.toString();
        super.setText(StateClient.displayServer(raw), type);
    }
}
