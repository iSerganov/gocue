#include <stdlib.h>
#include <libavutil/time.h>
#include "libav.h"

static int deadline_passed(void *opaque) {
    return av_gettime_relative() > ((gocue_input *)opaque)->deadline;
}

gocue_input *gocue_open_input(const char *path, int64_t timeout_us, int *err) {
    gocue_input *in = calloc(1, sizeof(*in));
    if (!in) {
        *err = AVERROR(ENOMEM);
        return NULL;
    }
    in->deadline = av_gettime_relative() + timeout_us;
    in->fmt = avformat_alloc_context();
    if (!in->fmt) {
        free(in);
        *err = AVERROR(ENOMEM);
        return NULL;
    }
    in->fmt->interrupt_callback.callback = deadline_passed;
    in->fmt->interrupt_callback.opaque = in;

    *err = avformat_open_input(&in->fmt, path, NULL, NULL);
    if (*err >= 0)
        *err = avformat_find_stream_info(in->fmt, NULL);
    if (*err < 0) {
        gocue_close_input(in);
        return NULL;
    }
    return in;
}

void gocue_close_input(gocue_input *in) {
    avformat_close_input(&in->fmt);
    free(in);
}
