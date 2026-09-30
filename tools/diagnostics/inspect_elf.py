import argparse
import struct
import re
from pathlib import Path
import capstone

parser = argparse.ArgumentParser()
parser.add_argument('file')
parser.add_argument('--symbols', default='')
parser.add_argument('--strings', default='')
parser.add_argument('--disasm', default='')
parser.add_argument('--size', type=lambda s: int(s, 0), default=256)
args = parser.parse_args()
b = Path(args.file).read_bytes()
bits = b[4]
if bits == 1:
    hdr = struct.unpack_from('<16sHHIIIIIHHHHHH', b)
    sfmt = '<IIIIIIIIII'
else:
    hdr = struct.unpack_from('<16sHHIQQQIHHHHHH', b)
    sfmt = '<IIQQQQIIQQ'
sections = [struct.unpack_from(sfmt, b, hdr[6] + i*hdr[11]) for i in range(hdr[12])]
names = sections[hdr[13]]
def z(offset): return b[offset:b.find(b'\0', offset)].decode('utf8', 'replace')
symbols = {}
symbol_tables = {}
for i,s in enumerate(sections):
    if s[1] not in (2,11): continue
    strings = sections[s[6]]
    table = []
    for off in range(s[4], s[4]+s[5], s[9]):
        if bits == 1:
            name,val,size,info,other,sec = struct.unpack_from('<IIIBBH', b, off)
        else:
            name,info,other,sec,val,size = struct.unpack_from('<IBBHQQ', b, off)
        label = z(strings[4]+name)
        table.append((label,val,size,sec))
        if label: symbols[label] = (val,size,sec)
        if args.symbols and (args.symbols == '*' or args.symbols.lower() in label.lower()):
            print(hex(val), hex(size), sec, label)
    symbol_tables[i] = table
if args.strings:
    needle = args.strings.encode()
    off = 0
    while (off := b.find(needle, off)) >= 0:
        start = b.rfind(b'\0',0,off)+1
        end = b.find(b'\0',off)
        address = next((s[3]+start-s[4] for s in sections if s[4] <= start < s[4]+s[5] and s[1] != 8), start)
        print(hex(address), b[start:end].decode('utf8','replace'))
        off += len(needle)
if args.disasm:
    if args.disasm in symbols:
        addr,size,sec = symbols[args.disasm]
        section = sections[sec]
        start = section[4]+addr-section[3]
        size = size or args.size
    else:
        addr=int(args.disasm,0)
        section=next(s for s in sections if s[3] <= addr < s[3]+s[5] and s[1] == 1 and s[2]&4)
        start=section[4]+addr-section[3]
        size=args.size
    arch = capstone.CS_ARCH_ARM64 if hdr[2] == 183 else capstone.CS_ARCH_ARM
    md=capstone.Cs(arch, capstone.CS_MODE_LITTLE_ENDIAN)
    reloc = {}
    for s in sections:
        if s[1] != 4 or sections[s[7]] != section: continue
        for off in range(s[4],s[4]+s[5],s[9]):
            target,info,addend = struct.unpack_from('<QQq',b,off)
            sym=symbol_tables[s[6]][info>>32]
            name=sym[0] or z(names[4]+sections[sym[3]][0])
            reloc[target] = name+f'+{addend:#x}'
    for ins in md.disasm(b[start:start+size],addr):
        label=next((n for n,(a,sz,se) in symbols.items() if a==ins.address and (args.disasm not in symbols or se==symbols[args.disasm][2])), '')
        note = reloc.get(ins.address, '')
        if hdr[1] != 1 and bits == 1:
            match=re.search(r'\[pc, #(0x[0-9a-f]+)\]',ins.op_str)
            if match:
                at=ins.address+8+int(match[1],16)
                sec=next((s for s in sections if s[3]<=at<s[3]+s[5] and s[1]==1),None)
                if sec:
                    ptr=struct.unpack_from('<I',b,sec[4]+at-sec[3])[0]
                    target=next((s for s in sections if s[3]<=ptr<s[3]+s[5] and s[1]==1),None)
                    note+=f' literal={ptr:#x}'
                    if target:
                        text=z(target[4]+ptr-target[3])
                        if text.isprintable() and len(text)<200: note+=' '+text
        print(f'{ins.address:08x} {ins.mnemonic:8} {ins.op_str:55} {label} {note}')
