# Feature provider contract

`FeatureProvider` exposes provider identity, availability, list-features, get-feature, and metadata operations. Local Spec Kit is the initial implementation and maps a feature packet directory to a feature read model. A provider never exposes a credential value in a returned model.

Future remote providers implement the same contract; they are out of scope for this feature packet.
