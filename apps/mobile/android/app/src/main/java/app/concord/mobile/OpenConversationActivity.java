package app.concord.mobile;

import android.app.Activity;
import android.content.Intent;
import android.net.Uri;
import android.os.Bundle;

/**
 * The owner's Jayb opens a conversation here (jaybos CONTRACTS.md §5): an
 * explicit start of this activity with concord://open?c=<id>. Exported, but
 * Android lets only an app holding OPEN_CONVERSATION (signature|knownSigner,
 * @array/card_readers) start it, and it has no BROWSABLE filter, so no web
 * page reaches it. It checks the link strictly, hands MainActivity a gated
 * concord://channel?id=<id> (OpenGate), and shows nothing itself. Anything
 * malformed just opens nothing. Whether the id is one of the owner's
 * conversations is the page's to decide; an unknown one opens nothing, quietly.
 */
public class OpenConversationActivity extends Activity {
    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        Intent in = getIntent();
        Uri data = in != null ? in.getData() : null;
        String id = data != null && Intent.ACTION_VIEW.equals(in.getAction()) ? OpenGate.openId(data.toString()) : null;
        if (id != null) {
            Intent go = new Intent(this, MainActivity.class)
                .setAction(Intent.ACTION_VIEW)
                .setData(Uri.parse("concord://channel?id=" + id))
                .addFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP | Intent.FLAG_ACTIVITY_CLEAR_TOP);
            startActivity(OpenGate.stamp(this, go));
        }
        finish();
    }
}
