package app.concord.mobile;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;

import org.junit.Test;

/**
 * The photo picker and the share sheet stage through ShareIntake: pictures
 * and videos only, ten at a time (the owner's limit for both).
 */
public class ShareIntakeTest {
    @Test
    public void takesPicturesAndVideosOnly() {
        assertTrue(ShareIntake.isMedia("image/jpeg"));
        assertTrue(ShareIntake.isMedia("image/heic"));
        assertTrue(ShareIntake.isMedia("video/mp4"));
        assertFalse(ShareIntake.isMedia("application/pdf"));
        assertFalse(ShareIntake.isMedia("text/plain"));
        assertFalse(ShareIntake.isMedia(""));
        assertFalse(ShareIntake.isMedia(null));
    }

    @Test
    public void tenItemsAtMost() {
        assertEquals(10, ShareIntake.MAX_ITEMS);
    }
}
