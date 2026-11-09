export function varint(value: number): Buffer {
  const bytes: number[] = [];
  let remaining = value;
  do {
    let byte = remaining % 0x80;
    remaining = Math.floor(remaining / 0x80);
    if (remaining > 0) {
      byte |= 0x80;
    }
    bytes.push(byte);
  } while (remaining > 0);
  return Buffer.from(bytes);
}

export function field(fieldNumber: number, value: Buffer): Buffer {
  return Buffer.concat([
    varint((fieldNumber << 3) | 2),
    varint(value.length),
    value,
  ]);
}
