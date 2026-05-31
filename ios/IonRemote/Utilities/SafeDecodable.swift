import Foundation

/// Wrapper that catches decode errors for individual elements in an array,
/// allowing the rest of the array to decode successfully.
///
/// Usage:
///   let raw = try container.decode([SafeDecodable<MyType>].self, forKey: .items)
///   let items = raw.compactMap(\.value)
struct SafeDecodable<T: Decodable>: Decodable {
    let value: T?

    init(from decoder: Decoder) throws {
        let container = try decoder.singleValueContainer()
        value = try? container.decode(T.self)
    }
}
