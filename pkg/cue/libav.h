#ifndef GOCUE_LIBAV_H
#define GOCUE_LIBAV_H

#include <stddef.h>
#include <stdint.h>
#include <libavformat/avformat.h>

typedef struct {
    AVFormatContext *fmt;
    int64_t deadline;
} gocue_input;

gocue_input *gocue_open_input(const char *path, int64_t timeout_us, int *err);
void gocue_close_input(gocue_input *in);

typedef struct {
    int64_t *pts;
    double *momentary;
    size_t n, cap;
    AVRational time_base;
    double integrated, true_peak, loudness_range;
} gocue_scan_result;

gocue_scan_result *gocue_scan(const char *path, double target, int64_t timeout_us, int *err);
void gocue_scan_free(gocue_scan_result *res);

#endif
