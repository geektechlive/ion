import Compression
import Foundation

/// Decompresses raw DEFLATE data (no gzip/zlib header) using Apple's Compression framework.
///
/// The desktop compresses outbound payloads with `zlib.deflateRawSync()` (raw DEFLATE)
/// and prepends a 0x01 version byte. After decryption, callers strip the version byte
/// and pass the remaining data here.
///
/// `COMPRESSION_ZLIB` in Apple's Compression framework handles raw DEFLATE (RFC 1951),
/// which is exactly what Node.js `deflateRawSync` produces.
enum PayloadCompression {

    /// Decompress raw DEFLATE data. Throws on failure.
    static func inflateRaw(_ data: Data) throws -> Data {
        // Start with 8× the input size as initial capacity (DEFLATE typically
        // compresses JSON 10–15×, so 8× is a conservative starting point).
        let inputCount = data.count
        var outputCapacity = inputCount * 8
        var output = Data(count: outputCapacity)

        let decompressedSize: Int = try data.withUnsafeBytes { inputPtr in
            guard let inputBase = inputPtr.baseAddress?.assumingMemoryBound(to: UInt8.self) else {
                throw CompressionError.emptyInput
            }
            return try output.withUnsafeMutableBytes { outputPtr in
                guard let outputBase = outputPtr.baseAddress?.assumingMemoryBound(to: UInt8.self) else {
                    throw CompressionError.bufferAllocationFailed
                }
                let result = compression_decode_buffer(
                    outputBase, outputCapacity,
                    inputBase, inputCount,
                    nil, // scratch buffer (nil = framework allocates internally)
                    COMPRESSION_ZLIB
                )
                guard result > 0 else {
                    throw CompressionError.decompressionFailed
                }
                return result
            }
        }

        // If the decompressed size exactly fills the buffer, the data may have
        // been truncated. Retry with a larger buffer.
        if decompressedSize == outputCapacity {
            outputCapacity = outputCapacity * 4
            output = Data(count: outputCapacity)
            let retrySize: Int = try data.withUnsafeBytes { inputPtr in
                guard let inputBase = inputPtr.baseAddress?.assumingMemoryBound(to: UInt8.self) else {
                    throw CompressionError.emptyInput
                }
                return try output.withUnsafeMutableBytes { outputPtr in
                    guard let outputBase = outputPtr.baseAddress?.assumingMemoryBound(to: UInt8.self) else {
                        throw CompressionError.bufferAllocationFailed
                    }
                    let result = compression_decode_buffer(
                        outputBase, outputCapacity,
                        inputBase, inputCount,
                        nil,
                        COMPRESSION_ZLIB
                    )
                    guard result > 0 else {
                        throw CompressionError.decompressionFailed
                    }
                    return result
                }
            }
            output.count = retrySize
        } else {
            output.count = decompressedSize
        }

        return output
    }

    enum CompressionError: Error, CustomStringConvertible {
        case emptyInput
        case bufferAllocationFailed
        case decompressionFailed

        var description: String {
            switch self {
            case .emptyInput: return "PayloadCompression: empty input data"
            case .bufferAllocationFailed: return "PayloadCompression: failed to allocate output buffer"
            case .decompressionFailed: return "PayloadCompression: COMPRESSION_ZLIB decompression returned 0"
            }
        }
    }
}
