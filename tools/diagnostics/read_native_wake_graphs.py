"""Read-only inspection of the verified L06A factory wakeword library.

Reads a local ELF file and prints JSON. Does not execute the ELF, connect to a
device, record audio, patch bytes, or create output files. Python stdlib only.
"""
import argparse
import hashlib
import json
import math
import struct
from pathlib import Path

EXPECTED_SHA256 = "ae3f7ca5502f74eb88d2001253cb0c3cd4fdb9d6e860c902b44964f0aa2dc65c"
MODELS = (("level1", 0xC47E8, 639597), ("level2", 0x160A58, 1678701))


def inspect(path):
    data = path.read_bytes()
    digest = hashlib.sha256(data).hexdigest()
    if digest != EXPECTED_SHA256:
        raise ValueError("Library SHA-256 differs; these addresses are version-specific")
    header = struct.unpack_from("<16sHHIIIIIHHHHHH", data)
    if data[:6] != b"\x7fELF\x01\x01" or header[2] != 40:
        raise ValueError("Expected little-endian ELF32 ARM")
    sections = [struct.unpack_from("<10I", data, header[6] + i * header[11])
                for i in range(header[12])]

    def offset(address):
        for section in sections:
            if section[1] != 8 and section[2] & 2 and section[3] <= address < section[3] + section[5]:
                return section[4] + address - section[3]
        raise ValueError("Address is outside file-backed allocated sections")

    models = []
    for name, address, size in MODELS:
        start = offset(address)
        model = data[start:start + size]
        fst_start = model.index(bytes.fromhex("d6fdb27e"), 0, 1600)
        if model[fst_start - 5:fst_start] != bytes.fromhex("c0000001ea"):
            raise ValueError("Expected a 490-byte Binn FST blob")
        fst = model[fst_start:fst_start + 490]
        cursor = 4

        def unpack(fmt):
            nonlocal cursor
            result = struct.unpack_from(fmt, fst, cursor)
            cursor += struct.calcsize(fmt)
            return result

        names = []
        for _ in range(2):
            length, = unpack("<i")
            names.append(fst[cursor:cursor + length].decode("ascii"))
            cursor += length
        version, flags, properties, initial, state_count, header_arc_count = unpack("<iiQqqq")
        if names != ["vector", "standard"] or version != 2 or flags != 0 or state_count != 10:
            raise ValueError("Unexpected FST header")
        states = []
        for state_id in range(state_count):
            final_weight, arc_count = unpack("<fq")
            arcs = []
            if not 0 <= arc_count <= 100:
                raise ValueError("Unexpected arc count")
            for _ in range(arc_count):
                ilabel, olabel, weight, target = unpack("<iifi")
                if not 0 <= target < state_count:
                    raise ValueError("Invalid arc target")
                arcs.append(dict(input=ilabel, output=olabel, weight=weight, target=target))
            states.append(dict(state=state_id, final_weight=final_weight if math.isfinite(final_weight) else None,
                               arcs=arcs))
        if cursor != len(fst):
            raise ValueError("FST not consumed exactly")
        models.append(dict(name=name, elf_virtual_address=hex(address), bytes=size,
                           sha256=hashlib.sha256(model).hexdigest(),
                           fst_file_offset=hex(start + fst_start), fst_bytes=len(fst),
                           fst_header_arc_count=header_arc_count,
                           parsed_arc_count=sum(len(s["arcs"]) for s in states),
                           states=states))
    return dict(library=str(path.resolve()), library_sha256=digest, models=models)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("library", nargs="?", type=Path,
                        default=Path(__file__).parent / "original-system1-rootfs/usr/lib/libxaudio_engine.so")
    args = parser.parse_args()
    print(json.dumps(inspect(args.library), ensure_ascii=False, indent=2, allow_nan=False))
