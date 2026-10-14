#ifndef GOCUE_LIBAV_H
#define GOCUE_LIBAV_H

#include <stdint.h>
#include <libavformat/avformat.h>

typedef struct {
    AVFormatContext *fmt;
    int64_t deadline;
} gocue_input;

gocue_input *gocue_open_input(const char *path, int64_t timeout_us, int *err);
void gocue_close_input(gocue_input *in);

#endif
