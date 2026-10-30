const BermudaQR = (function () {
  const RS_BLOCKS_M = [
    [1, 26, 16], [1, 44, 28], [1, 70, 44], [2, 50, 32], [2, 67, 43],
    [4, 43, 27], [4, 49, 31], [2, 60, 38, 2, 61, 39], [3, 58, 36, 2, 59, 37],
    [4, 69, 43, 1, 70, 44], [1, 80, 50, 4, 81, 51], [6, 58, 36, 2, 59, 37],
    [8, 59, 37, 1, 60, 38], [4, 64, 40, 5, 65, 41], [5, 65, 41, 5, 66, 42],
    [7, 73, 45, 3, 74, 46], [10, 74, 46, 1, 75, 47], [9, 69, 43, 4, 70, 44],
    [3, 70, 44, 11, 71, 45], [3, 67, 41, 13, 68, 42], [17, 68, 42],
    [17, 74, 46], [4, 75, 47, 14, 76, 48], [6, 73, 45, 14, 74, 46],
    [8, 75, 47, 13, 76, 48], [19, 74, 46, 4, 75, 47], [22, 73, 45, 3, 74, 46],
    [3, 73, 45, 23, 74, 46], [21, 73, 45, 7, 74, 46], [19, 75, 47, 10, 76, 48],
    [2, 74, 46, 29, 75, 47], [10, 74, 46, 23, 75, 47], [14, 74, 46, 21, 75, 47],
    [14, 74, 46, 23, 75, 47], [12, 75, 47, 26, 76, 48], [6, 75, 47, 34, 76, 48],
    [29, 74, 46, 14, 75, 47], [13, 74, 46, 32, 75, 47], [40, 75, 47, 7, 76, 48],
    [18, 75, 47, 31, 76, 48]
  ];

  const ALIGN = [
    [], [6, 18], [6, 22], [6, 26], [6, 30], [6, 34],
    [6, 22, 38], [6, 24, 42], [6, 26, 46], [6, 28, 50], [6, 30, 54],
    [6, 32, 58], [6, 34, 62], [6, 26, 46, 66], [6, 26, 48, 70],
    [6, 26, 50, 74], [6, 30, 54, 78], [6, 30, 56, 82], [6, 30, 58, 86],
    [6, 34, 62, 90], [6, 28, 50, 72, 94], [6, 26, 50, 74, 98],
    [6, 30, 54, 78, 102], [6, 28, 54, 80, 106], [6, 32, 58, 84, 110],
    [6, 30, 58, 86, 114], [6, 34, 62, 90, 118], [6, 26, 50, 74, 98, 122],
    [6, 30, 54, 78, 102, 126], [6, 26, 52, 78, 104, 130],
    [6, 30, 56, 82, 108, 134], [6, 34, 60, 86, 112, 138],
    [6, 30, 58, 86, 114, 142], [6, 34, 62, 90, 118, 146],
    [6, 30, 54, 78, 102, 126, 150], [6, 24, 50, 76, 102, 128, 154],
    [6, 28, 54, 80, 106, 132, 158], [6, 32, 58, 84, 110, 136, 162],
    [6, 26, 54, 82, 110, 138, 166], [6, 30, 58, 86, 114, 142, 170]
  ];

  const EXP = new Uint8Array(512), LOG = new Uint8Array(256);
  (function initGF() {
    let x = 1;
    for (let i = 0; i < 255; i++) { EXP[i] = x; LOG[x] = i; x <<= 1; if (x & 0x100) x ^= 0x11D; }
    for (let i = 255; i < 512; i++) EXP[i] = EXP[i - 255];
  })();

  function gmul(a, b) { return (a === 0 || b === 0) ? 0 : EXP[LOG[a] + LOG[b]]; }
  function rsGenerator(n) {
    let g = [1];
    for (let i = 0; i < n; i++) {
      const ng = new Array(g.length + 1).fill(0), root = EXP[i];
      for (let j = 0; j < g.length; j++) { ng[j] ^= gmul(g[j], root); ng[j + 1] ^= g[j]; }
      g = ng;
    }
    return g;
  }
  function rsEncode(data, ecLen) {
    const gen = rsGenerator(ecLen), res = new Uint8Array(ecLen);
    for (let i = 0; i < data.length; i++) {
      const feedback = data[i] ^ res[0];
      for (let j = 0; j < ecLen - 1; j++) res[j] = res[j + 1] ^ gmul(gen[ecLen - 1 - j], feedback);
      res[ecLen - 1] = gmul(gen[0], feedback);
    }
    return res;
  }

  function BitBuffer() { this.buffer = []; this.length = 0; }
  BitBuffer.prototype.put = function (num, count) { for (let i = 0; i < count; i++) this.putBit(((num >>> (count - i - 1)) & 1) === 1); };
  BitBuffer.prototype.putBit = function (bit) {
    const bufIndex = Math.floor(this.length / 8);
    if (this.buffer.length <= bufIndex) this.buffer.push(0);
    if (bit) this.buffer[bufIndex] |= (0x80 >>> (this.length % 8));
    this.length++;
  };

  function dataCapacity(version) {
    const desc = RS_BLOCKS_M[version - 1]; let total = 0;
    for (let i = 0; i < desc.length; i += 3) total += desc[i] * desc[i + 2];
    return total;
  }

  function chooseVersion(byteLen, utf8ECI = false) {
    for (let v = 1; v <= 40; v++) {
      const cap = dataCapacity(v), overhead = (v < 10 ? 12 : 20) + (utf8ECI ? 12 : 0);
      if (overhead + byteLen * 8 <= cap * 8) return v;
    }
    return 0;
  }

  function encodeData(str, version) {
    const bytes = new TextEncoder().encode(str), hasUnicode = /[^\x00-\x7F]/.test(str), bb = new BitBuffer();
    if (hasUnicode) { bb.put(7, 4); bb.put(26, 8); }
    bb.put(4, 4); bb.put(bytes.length, version < 10 ? 8 : 16);
    for (let i = 0; i < bytes.length; i++) bb.put(bytes[i], 8);
    const totalDataBits = dataCapacity(version) * 8;
    if (bb.length + 4 <= totalDataBits) bb.put(0, 4); else if (bb.length < totalDataBits) bb.put(0, totalDataBits - bb.length);
    while (bb.length % 8 !== 0) bb.putBit(false);
    let padByte = 0xEC;
    while (bb.length < totalDataBits) { bb.put(padByte, 8); padByte = padByte === 0xEC ? 0x11 : 0xEC; }
    return bb.buffer;
  }

  function interleave(dataBytes, version) {
    const desc = RS_BLOCKS_M[version - 1], dataBlocks = [], ecBlocks = [];
    let offset = 0;
    for (let i = 0; i < desc.length; i += 3) {
      const count = desc[i], totalLen = desc[i + 1], dataLen = desc[i + 2], ecLen = totalLen - dataLen;
      for (let c = 0; c < count; c++) {
        const d = dataBytes.slice(offset, offset + dataLen); offset += dataLen;
        dataBlocks.push(d); ecBlocks.push(rsEncode(d, ecLen));
      }
    }
    const result = [];
    let maxDataLen = 0; for (let i = 0; i < dataBlocks.length; i++) if (dataBlocks[i].length > maxDataLen) maxDataLen = dataBlocks[i].length;
    for (let j = 0; j < maxDataLen; j++) for (let i = 0; i < dataBlocks.length; i++) if (j < dataBlocks[i].length) result.push(dataBlocks[i][j]);
    const maxEcLen = ecBlocks[0].length;
    for (let j = 0; j < maxEcLen; j++) for (let i = 0; i < ecBlocks.length; i++) result.push(ecBlocks[i][j]);
    return result;
  }

  const MASKS = [
    (i, j) => ((i + j) % 2) === 0, (i, j) => (i % 2) === 0, (i, j) => (j % 3) === 0, (i, j) => ((i + j) % 3) === 0,
    (i, j) => ((Math.floor(i / 2) + Math.floor(j / 3)) % 2) === 0, (i, j) => ((i * j) % 2 + (i * j) % 3) === 0,
    (i, j) => (((i * j) % 2 + (i * j) % 3) % 2) === 0, (i, j) => (((i * j) % 3 + (i + j) % 2) % 2) === 0
  ];

  function createMatrix(version) {
    const size = version * 4 + 17, m = [], reserved = [];
    for (let i = 0; i < size; i++) { m.push(new Uint8Array(size)); reserved.push(new Uint8Array(size)); }
    function setFinder(r, c) {
      for (let i = -1; i <= 7; i++) {
        for (let j = -1; j <= 7; j++) {
          const row = r + i, col = c + j;
          if (row < 0 || row >= size || col < 0 || col >= size) continue;
          reserved[row][col] = 1;
          if (i >= 0 && i <= 6 && (j === 0 || j === 6)) m[row][col] = 1;
          else if (j >= 0 && j <= 6 && (i === 0 || i === 6)) m[row][col] = 1;
          else if (i >= 2 && i <= 4 && j >= 2 && j <= 4) m[row][col] = 1;
          else m[row][col] = 0;
        }
      }
    }
    setFinder(0, 0); setFinder(0, size - 7); setFinder(size - 7, 0);
    for (let i = 8; i < size - 8; i++) { reserved[6][i] = 1; m[6][i] = (i % 2 === 0) ? 1 : 0; reserved[i][6] = 1; m[i][6] = (i % 2 === 0) ? 1 : 0; }
    if (version >= 2) {
      const coords = ALIGN[version - 1];
      for (let i = 0; i < coords.length; i++) {
        for (let j = 0; j < coords.length; j++) {
          const r = coords[i], c = coords[j];
          if ((i === 0 && j === 0) || (i === 0 && j === coords.length - 1) || (j === 0 && i === coords.length - 1)) continue;
          for (let di = -2; di <= 2; di++) {
            for (let dj = -2; dj <= 2; dj++) {
              reserved[r + di][c + dj] = 1;
              m[r + di][c + dj] = (Math.abs(di) === 2 || Math.abs(dj) === 2 || (di === 0 && dj === 0)) ? 1 : 0;
            }
          }
        }
      }
    }
    for (let i = 0; i < 9; i++) { if (i < size) { reserved[8][i] = 1; reserved[i][8] = 1; } }
    for (let i = 0; i < 8; i++) { reserved[8][size - 1 - i] = 1; reserved[size - 1 - i][8] = 1; }
    reserved[size - 8][8] = 1; m[size - 8][8] = 1;
    if (version >= 7) {
      let rem = version;
      for (let i = 0; i < 12; i++) rem = (rem << 1) ^ ((rem >>> 11) * 0x1f25);
      const bits = (version << 12) | rem;
      for (let i = 0; i < 18; i++) {
        const a = size - 11 + i % 3, b = Math.floor(i / 3);
        reserved[a][b] = reserved[b][a] = 1; m[a][b] = m[b][a] = (bits >>> i) & 1;
      }
    }
    return { size, m, reserved };
  }

  const FORMAT_BITS_M = [0x5412, 0x5125, 0x5E7C, 0x5B4B, 0x45F9, 0x40CE, 0x4F97, 0x4AA0];
  function placeFormat(matrix, maskIdx) {
    const bits = FORMAT_BITS_M[maskIdx], size = matrix.size, m = matrix.m;
    for (let i = 0; i < 15; i++) {
      const bit = (bits >>> i) & 1;
      if (i < 6) m[i][8] = bit; else if (i < 8) m[i + 1][8] = bit; else m[size - 15 + i][8] = bit;
      if (i < 8) m[8][size - i - 1] = bit; else if (i < 9) m[8][15 - i] = bit; else m[8][14 - i] = bit;
    }
    m[size - 8][8] = 1;
  }

  function penalty(m) {
    const n = m.length; let score = 0, dark = 0;
    for (let axis = 0; axis < 2; axis++) {
      for (let a = 0; a < n; a++) {
        let run = 0, last = -1, line = '';
        for (let b = 0; b < n; b++) {
          const v = axis ? m[b][a] : m[a][b]; line += v;
          if (v === last) run++; else { if (run >= 5) score += run - 2; run = 1; last = v; }
        }
        if (run >= 5) score += run - 2;
        for (let b = 0; b <= n - 11; b++) {
          const sub = line.slice(b, b + 11); if (sub === '00001011101' || sub === '10111010000') score += 40;
        }
      }
    }
    for (let r = 0; r < n - 1; r++) {
      for (let c = 0; c < n - 1; c++) {
        const val = m[r][c]; if (val === m[r + 1][c] && val === m[r][c + 1] && val === m[r + 1][c + 1]) score += 3;
      }
    }
    for (let r = 0; r < n; r++) for (let c = 0; c < n; c++) dark += m[r][c];
    score += Math.floor(Math.abs(dark * 100 / (n * n) - 50) / 5) * 10;
    return score;
  }

  function renderSVG(str) {
    const rawLen = new TextEncoder().encode(str).length, hasUnicode = /[^\x00-\x7F]/.test(str);
    const version = chooseVersion(rawLen, hasUnicode); if (!version) return { error: "DATA_TOO_LARGE" };
    const dataBytes = encodeData(str, version), finalStream = interleave(dataBytes, version);
    const matrix = createMatrix(version), size = matrix.size, m = matrix.m, res = matrix.reserved;
    let byteIdx = 0, bitIdx = 7, upwards = true;
    for (let right = size - 1; right > 0; right -= 2) {
      if (right === 6) right--;
      for (let v = 0; v < size; v++) {
        const row = upwards ? (size - 1 - v) : v;
        for (let col = right; col >= right - 1; col--) {
          if (!res[row][col]) {
            let bit = 0;
            if (byteIdx < finalStream.length) {
              bit = (finalStream[byteIdx] >>> bitIdx) & 1; bitIdx--;
              if (bitIdx < 0) { bitIdx = 7; byteIdx++; }
            }
            m[row][col] = bit;
          }
        }
      }
      upwards = !upwards;
    }
    const original = m.map(row => row.slice()); let best = null, bestScore = Infinity;
    for (let maskIdx = 0; maskIdx < 8; maskIdx++) {
      for (let i = 0; i < size; i++) for (let j = 0; j < size; j++) m[i][j] = original[i][j] ^ ((!res[i][j] && MASKS[maskIdx](i,j)) ? 1 : 0);
      placeFormat(matrix, maskIdx); const score = penalty(m);
      if (score < bestScore) { bestScore = score; best = m.map(row => row.slice()); }
    }
    for (let i = 0; i < size; i++) m[i].set(best[i]);
    return { matrix: m, size, version };
  }

  function toSVG(str) {
    const res = renderSVG(str); if (res.error) return { error: res.error };
    const size = res.size, m = res.matrix, border = 4, totalSize = size + border * 2;
    let path = "";
    for (let i = 0; i < size; i++) {
      for (let j = 0; j < size; j++) if (m[i][j] === 1) path += "M" + (j + border) + "," + (i + border) + "h1v1h-1z ";
    }
    return {
      svg: '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ' + totalSize + ' ' + totalSize + '" ' +
        'width="' + totalSize * 4 + '" height="' + totalSize * 4 + '" shape-rendering="crispEdges" style="display:block;max-width:100%;">' +
        '<rect width="100%" height="100%" fill="#ffffff"/>' +
        '<path d="' + path.trim() + '" fill="#000000"/>' +
        '</svg>',
      version: res.version
    };
  }

  return { toSVG };
})();
if (typeof window !== "undefined") window.BermudaQR = BermudaQR;
if (typeof module !== "undefined" && module.exports) module.exports = BermudaQR;
