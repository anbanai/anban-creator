"""Map the pinned upstream preview font to the Debian runtime font."""

import os


_UPSTREAM_WINDOWS_FONT = "C:/Windows/Fonts/msyh.ttc"
_LINUX_FALLBACK_FONT = os.environ.get(
    "ANBAN_WHITEBOARD_FONT", "/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf"
)


def _patch_image_font(image_font):
    if not hasattr(image_font, "truetype"):
        return
    if getattr(image_font, "_anban_whiteboard_font_patched", False):
        return
    original_truetype = image_font.truetype

    def _truetype(font, size, *args, **kwargs):
        if font == _UPSTREAM_WINDOWS_FONT:
            font = _LINUX_FALLBACK_FONT
        return original_truetype(font, size, *args, **kwargs)

    image_font.truetype = _truetype
    image_font._anban_whiteboard_font_patched = True


try:
    from PIL import ImageFont
except ImportError:
    pass
else:
    _patch_image_font(ImageFont)
