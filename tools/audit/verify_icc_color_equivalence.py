#!/usr/bin/env python3
"""Compare the named RGB ICC fixture before/after metadata projection.

Requires Pillow with LittleCMS. Reports hashes and booleans, never source
profile descriptions or other free metadata. No network or database access.
"""
import argparse
import hashlib
import json
from pathlib import Path

from PIL import Image, ImageCms


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source", type=Path, required=True)
    parser.add_argument("--sanitized", type=Path, required=True)
    parser.add_argument("--output", type=Path)
    args = parser.parse_args()
    try:
        source = ImageCms.ImageCmsProfile(str(args.source))
        sanitized = ImageCms.ImageCmsProfile(str(args.sanitized))
        sample = Image.new("RGB", (17, 17))
        sample.putdata([(i % 256, (i * 71) % 256, (i * 137) % 256) for i in range(289)])
        target = ImageCms.createProfile("sRGB")
        original_rgb = ImageCms.profileToProfile(sample, source, target, outputMode="RGB").tobytes()
        sanitized_rgb = ImageCms.profileToProfile(sample, sanitized, target, outputMode="RGB").tobytes()
        result = {
            "source_icc_sha256": hashlib.sha256(args.source.read_bytes()).hexdigest(),
            "sanitized_icc_sha256": hashlib.sha256(args.sanitized.read_bytes()).hexdigest(),
            "source_profile_load": "PASS",
            "sanitized_profile_load": "PASS",
            "sanitized_description_is_generic": ImageCms.getProfileDescription(sanitized).strip() == "Catalog color profile",
            "RGB_samples": 289,
            "color_conversion_exact_equal": original_rgb == sanitized_rgb,
            "RGB_color_result": "PASS" if original_rgb == sanitized_rgb else "FAIL",
            "engine": ImageCms.core.littlecms_version,
        }
    except Exception:
        # Profile library exceptions can contain paths or free-text metadata.
        result = {"RGB_color_result": "FAIL", "reason": "PROFILE_LOAD_OR_TRANSFORM_FAILED"}
    encoded = json.dumps(result, indent=2) + "\n"
    if args.output:
        args.output.write_text(encoded)
    print(encoded, end="")
    return 0 if result["RGB_color_result"] == "PASS" and result.get("sanitized_description_is_generic") else 1


if __name__ == "__main__":
    raise SystemExit(main())
