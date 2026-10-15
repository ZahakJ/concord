package app.concord.mobile;

import android.content.ContentResolver;
import android.content.Context;
import android.content.Intent;
import android.content.pm.ProviderInfo;
import android.database.Cursor;
import android.net.Uri;
import android.os.Build;
import android.provider.OpenableColumns;
import android.webkit.MimeTypeMap;

import com.getcapacitor.JSArray;
import com.getcapacitor.JSObject;

import java.io.File;
import java.io.FileOutputStream;
import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.util.ArrayList;
import java.util.List;
import java.util.UUID;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;

/**
 * Pictures and videos shared INTO Concord from another app's share sheet.
 *
 * The bytes arrive as content:// URIs that only this activity has been granted
 * to read, and only for a while. The page cannot open them, so they are copied
 * here — off the main thread, under the composer's own 25 MB file limit — into
 * a private cache directory, and the page is handed each copy's path. It
 * fetches the copy through Capacitor's local file route (convertFileSrc), turns
 * it into a File and stages it through the composer's ordinary attachFile, the
 * same door a picked or dropped file goes through; then it asks for the copies
 * to be deleted (releaseShared). Anything left behind by a page that never got
 * that far is cleared the next time the plugin loads.
 *
 * Nothing is sent: a shared picture lands as a staged attachment the user can
 * look at, caption, or remove before pressing Send.
 */
final class ShareIntake {
    /** Composer.svelte's MAX_FILE_BYTES — the largest attachment it will stage. */
    static final long MAX_BYTES = 25L * 1024 * 1024;
    /** A share sheet multi-select is a handful of pictures, not an album. */
    static final int MAX_ITEMS = 10;

    private static final String DIR = "share-in";
    private static final ExecutorService exec = Executors.newSingleThreadExecutor();

    private ShareIntake() {}

    static File dir(Context ctx) {
        return new File(ctx.getCacheDir(), DIR);
    }

    /**
     * True for an intent this class should take: a SEND or SEND_MULTIPLE of
     * pictures or videos. Text shares (text/*, even ones that also carry a
     * stream) stay on the text path in MainActivity.
     */
    static boolean wants(Intent intent) {
        if (intent == null) return false;
        String action = intent.getAction();
        if (!Intent.ACTION_SEND.equals(action) && !Intent.ACTION_SEND_MULTIPLE.equals(action)) return false;
        return isMedia(intent.getType());
    }

    /**
     * Reads the intent's streams on a worker thread and emits one "shareIn"
     * event with the staged copies. The stream extras are removed before
     * returning so a re-delivered intent cannot stage the same share twice.
     */
    static void take(Context ctx, Intent intent) {
        final List<Uri> uris = new ArrayList<>();
        if (Intent.ACTION_SEND_MULTIPLE.equals(intent.getAction())) {
            ArrayList<Uri> list = streamsOf(intent);
            if (list != null) uris.addAll(list);
        } else {
            Uri u = streamOf(intent);
            if (u != null) uris.add(u);
        }
        String text = intent.getStringExtra(Intent.EXTRA_TEXT);
        final String caption = text != null ? text.trim() : "";
        final String intentType = intent.getType();
        intent.removeExtra(Intent.EXTRA_STREAM);
        intent.removeExtra(Intent.EXTRA_TEXT);
        if (uris.isEmpty() && caption.isEmpty()) return;

        final Context app = ctx.getApplicationContext();
        exec.execute(() -> {
            JSArray files = new JSArray();
            JSArray skipped = new JSArray();
            int n = 0;
            for (Uri uri : uris) {
                if (n >= MAX_ITEMS) {
                    skipped.put(skip(null, "count"));
                    continue;
                }
                JSObject item = copy(app, uri, intentType, skipped);
                if (item != null) {
                    files.put(item);
                    n++;
                }
            }
            JSObject data = new JSObject();
            if (!caption.isEmpty()) data.put("text", caption);
            data.put("files", files);
            if (skipped.length() > 0) data.put("skipped", skipped);
            ConcordCorePlugin.emitShare(data);
        });
    }

    /** Deletes staged copies the page has finished with. Only ever inside DIR. */
    static void release(Context ctx, List<String> paths) {
        File root;
        try {
            root = dir(ctx).getCanonicalFile();
        } catch (IOException e) {
            return;
        }
        for (String p : paths) {
            if (p == null) continue;
            try {
                File f = new File(p).getCanonicalFile();
                if (root.equals(f.getParentFile())) f.delete();
            } catch (IOException ignored) {
                // not ours to touch
            }
        }
    }

    /** Clears every copy — run at plugin load, before any share of this launch is taken. */
    static void clearAll(Context ctx) {
        File[] left = dir(ctx).listFiles();
        if (left == null) return;
        for (File f : left) f.delete();
    }

    @SuppressWarnings("deprecation")
    private static Uri streamOf(Intent intent) {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            return intent.getParcelableExtra(Intent.EXTRA_STREAM, Uri.class);
        }
        return intent.getParcelableExtra(Intent.EXTRA_STREAM);
    }

    @SuppressWarnings("deprecation")
    private static ArrayList<Uri> streamsOf(Intent intent) {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            return intent.getParcelableArrayListExtra(Intent.EXTRA_STREAM, Uri.class);
        }
        return intent.getParcelableArrayListExtra(Intent.EXTRA_STREAM);
    }

    private static JSObject skip(String name, String reason) {
        JSObject o = new JSObject();
        if (name != null) o.put("name", name);
        o.put("reason", reason);
        return o;
    }

    private static JSObject copy(Context ctx, Uri uri, String intentType, JSArray skipped) {
        if (uri == null) return null;
        // content:// only. A file:// URI names a path, and a share sheet is a
        // door any installed app can knock on: "share" this app's own private
        // files (keys, the database) and they would be staged as an attachment
        // one tap from being sent. For the same reason a content URI served by
        // one of this app's OWN providers is refused — the FileProvider exposes
        // the cache directory, and nothing legitimate shares Concord's own
        // files back into Concord.
        if (!ContentResolver.SCHEME_CONTENT.equals(uri.getScheme()) || ownAuthority(ctx, uri.getAuthority())) {
            skipped.put(skip(null, "unreadable"));
            return null;
        }
        ContentResolver cr = ctx.getContentResolver();
        String name = null;
        long declared = -1;
        try (Cursor c = cr.query(uri, new String[] { OpenableColumns.DISPLAY_NAME, OpenableColumns.SIZE }, null, null, null)) {
            if (c != null && c.moveToFirst()) {
                int ni = c.getColumnIndex(OpenableColumns.DISPLAY_NAME);
                int si = c.getColumnIndex(OpenableColumns.SIZE);
                if (ni >= 0 && !c.isNull(ni)) name = c.getString(ni);
                if (si >= 0 && !c.isNull(si)) declared = c.getLong(si);
            }
        } catch (Exception ignored) {
            // Metadata is a nicety; the stream is what matters.
        }
        String type = null;
        try {
            type = cr.getType(uri);
        } catch (Exception ignored) {
            // fall back to the intent's own type below
        }
        // The resolver's per-item answer first (a multi-select says only
        // "image/*" on the intent); the intent's type when it is specific and
        // the provider would not say.
        if (!isMedia(type) && isMedia(intentType) && !intentType.endsWith("/*")) type = intentType;
        if (!isMedia(type)) {
            skipped.put(skip(name, "type"));
            return null;
        }
        if (name == null || name.trim().isEmpty()) {
            String ext = MimeTypeMap.getSingleton().getExtensionFromMimeType(type);
            name = "shared" + (ext != null ? "." + ext : "");
        }
        if (declared > MAX_BYTES) {
            skipped.put(skip(name, "size"));
            return null;
        }

        File dir = dir(ctx);
        if (!dir.isDirectory() && !dir.mkdirs()) {
            skipped.put(skip(name, "unreadable"));
            return null;
        }
        File out = new File(dir, UUID.randomUUID().toString());
        long total = 0;
        try (InputStream in = cr.openInputStream(uri); OutputStream os = new FileOutputStream(out)) {
            if (in == null) throw new IOException("no stream");
            byte[] buf = new byte[64 * 1024];
            int r;
            while ((r = in.read(buf)) != -1) {
                total += r;
                // The declared size is the provider's word; the count is ours.
                if (total > MAX_BYTES) throw new TooLarge();
                os.write(buf, 0, r);
            }
        } catch (TooLarge e) {
            out.delete();
            skipped.put(skip(name, "size"));
            return null;
        } catch (Exception e) {
            out.delete();
            skipped.put(skip(name, "unreadable"));
            return null;
        }
        if (total == 0) {
            out.delete();
            skipped.put(skip(name, "unreadable"));
            return null;
        }
        JSObject o = new JSObject();
        o.put("path", out.getAbsolutePath());
        o.put("name", name);
        o.put("type", type);
        o.put("size", total);
        return o;
    }

    private static boolean isMedia(String type) {
        return type != null && (type.startsWith("image/") || type.startsWith("video/"));
    }

    private static boolean ownAuthority(Context ctx, String authority) {
        if (authority == null) return true;
        try {
            ProviderInfo pi = ctx.getPackageManager().resolveContentProvider(authority, 0);
            return pi != null && ctx.getPackageName().equals(pi.packageName);
        } catch (Exception e) {
            return true;
        }
    }

    private static final class TooLarge extends IOException {}
}
