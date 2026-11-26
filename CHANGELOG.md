# Changelog

## [3.2.0](https://github.com/create2-labs/cafe-persistence/compare/v3.1.0...v3.2.0) (2026-11-26)


### Features

* **scan:** create a missing enrichment in one transaction ([5eae9ab](https://github.com/create2-labs/cafe-persistence/commit/5eae9ab452053089e10d1f959803ccfe8fd32fd7))
* **scan:** create a missing enrichment in one transaction ([173f15f](https://github.com/create2-labs/cafe-persistence/commit/173f15f312deeae16a0bebf4e2d5e0dbb84b96fa))
* **scan:** list open enrichments for the abandon ([da1c049](https://github.com/create2-labs/cafe-persistence/commit/da1c049a44b6729087ae757dc483a56c010871ac))
* **scan:** list open enrichments for the abandon ([1cefdf3](https://github.com/create2-labs/cafe-persistence/commit/1cefdf36e5d2e81842781f8158515e77a3705812))
* **scan:** serve the enrichment transition route for pending enrichments ([6a33c1a](https://github.com/create2-labs/cafe-persistence/commit/6a33c1a190a790f680a94bd89c12b02d09916b74))
* **scan:** serve the enrichment transition route for pending enrichments ([aa3beaf](https://github.com/create2-labs/cafe-persistence/commit/aa3beaf19bba747b4a84f23d610d62d9593153e4))


### Bug Fixes

* **scan:** drop chains from the open-enrichment contract ([4964f6a](https://github.com/create2-labs/cafe-persistence/commit/4964f6a722079a4a825d6fa0e81fc2f17290acd6))
* **scan:** drop chains from the open-enrichment contract ([5d480fa](https://github.com/create2-labs/cafe-persistence/commit/5d480fa16aa6dd257c36d2b635dd6bb2d879396b))

## [3.1.0](https://github.com/create2-labs/cafe-persistence/compare/v3.0.0...v3.1.0) (2026-11-25)


### Features

* **scan:** add the scan_enrichments table ([a9bccdd](https://github.com/create2-labs/cafe-persistence/commit/a9bccdd48b55e001afb6e7eddfcfbb7be814e370))
* **scan:** add the scan_enrichments table ([03b23ec](https://github.com/create2-labs/cafe-persistence/commit/03b23ec317d922e9f7b62d31340324c7cddbfabe))
* **scan:** build wallet.observed from the stored qualification ([c73852f](https://github.com/create2-labs/cafe-persistence/commit/c73852f194f5d9f79c6ffa880b89d6b6d55158ee))
* **scan:** document the enrichment transition route ([89f1610](https://github.com/create2-labs/cafe-persistence/commit/89f161045ebb08264353f62cfa97febbebe7d2d3))
* **scan:** document the enrichment transition route ([aabc34f](https://github.com/create2-labs/cafe-persistence/commit/aabc34f600943f9485d1a99d74fa4f1aa24adb83))
* **scan:** document the open-enrichment route ([fcde59a](https://github.com/create2-labs/cafe-persistence/commit/fcde59a61f8c8391fb6f62cd45fdfbe27b2a0b8d))
* **scan:** document the open-enrichment route ([d5b138a](https://github.com/create2-labs/cafe-persistence/commit/d5b138ab6da08d9b83100436785b1c5ca11364fc))
* **scan:** publish wallet.observed only for an eoa ([85a8731](https://github.com/create2-labs/cafe-persistence/commit/85a87312c415f8a74ef90f23f7defc260d901192))
* **scan:** publish wallet.observed only for an eoa ([e9b587d](https://github.com/create2-labs/cafe-persistence/commit/e9b587ddd1069bcacf9eb37724c57cc98d0d6b5e))
* **scan:** query open enrichments ([0dc8fd4](https://github.com/create2-labs/cafe-persistence/commit/0dc8fd46bc136ab042a6302a729229384a9dacfc))
* **scan:** query open enrichments ([240efd3](https://github.com/create2-labs/cafe-persistence/commit/240efd3cca63665c61d8fc543113f7b27d89b7a9))
* **scan:** soft-delete related addresses and enrichments with their scan ([9cbf5b0](https://github.com/create2-labs/cafe-persistence/commit/9cbf5b0a79f137b7845a44a8d7cd823fabd4be24))
* **scan:** soft-delete related addresses and enrichments with their scan ([0603c2c](https://github.com/create2-labs/cafe-persistence/commit/0603c2c369505779cec7ce9d3b7c5cf12a09104e))


### Bug Fixes

* ci error ([14eed06](https://github.com/create2-labs/cafe-persistence/commit/14eed06ae0d1b5e54c7cb8293d00662e73fa4ebf))
* go test is already run in CI image ([827ceae](https://github.com/create2-labs/cafe-persistence/commit/827ceae740abb2db12060ad3b09dd8afe1a1084a))

## [3.0.0](https://github.com/create2-labs/cafe-persistence/compare/v2.2.0...v3.0.0) (2026-11-18)


### ⚠ BREAKING CHANGES

* **scan:** return the qualification columns in the wallet scan row
* **scan:** the internal wallet transition route accepts only an observation/address/v1 result for a SUCCESS; a result in the former shape is rejected.
* **scan:** store the address qualification and related addresses

### Features

* **scan:** filter wallet scans by their observed chains ([a01b07b](https://github.com/create2-labs/cafe-persistence/commit/a01b07b6c6b9ae584d73b424e91c91c697187b36))
* **scan:** filter wallet scans by their observed chains ([ff2e76c](https://github.com/create2-labs/cafe-persistence/commit/ff2e76c0849de27cd80a9f720f3e4c07a8479fd0))
* **scan:** return related addresses in the wallet scan by id ([34ca879](https://github.com/create2-labs/cafe-persistence/commit/34ca8795c26b51be1c5c9019cffc144046699747))
* **scan:** return related addresses in the wallet scan by id ([cfc563a](https://github.com/create2-labs/cafe-persistence/commit/cfc563aaf53d32274582c10ece992db6362a658b))
* **scan:** return the qualification columns in the wallet scan row ([03e2aa8](https://github.com/create2-labs/cafe-persistence/commit/03e2aa8b56a985c8afe5699b4441839b8f64941b))
* **scan:** store the address qualification and related addresses ([34d2e4a](https://github.com/create2-labs/cafe-persistence/commit/34d2e4ad766849cedc438a9b79c5446822e6778a))
* **scan:** store the delegation targets of a wallet transition ([23f9920](https://github.com/create2-labs/cafe-persistence/commit/23f992003ad85348c3c82c4e6bf0e4eb6aa7f544))
* **scan:** store the delegation targets of a wallet transition ([32f7f32](https://github.com/create2-labs/cafe-persistence/commit/32f7f327890bceef11f2f523e5b4770edfb7e015))
* **scan:** write the qualification in the wallet transition ([4908350](https://github.com/create2-labs/cafe-persistence/commit/4908350623fee2aff988e39288f6f022c109b4a0))


### Bug Fixes

* **config:** remove the development database credential ([2797dfc](https://github.com/create2-labs/cafe-persistence/commit/2797dfcd660fef36e7bbb355a63c88bc91ba9d6d))
* **config:** remove the development database credential from the tracked configuration ([a54ac46](https://github.com/create2-labs/cafe-persistence/commit/a54ac46bed2c355f1acec454863a0810013b2e59))
* govulncheck version ([963d756](https://github.com/create2-labs/cafe-persistence/commit/963d756730e843a678cf46701673380d37aae9f1))
* **postgres:** refuse an unset database credential ([5846048](https://github.com/create2-labs/cafe-persistence/commit/58460484d1465a55cc62e74ca7916af08d2ae244))
* **postgres:** refuse an unset database credential and pass the settings exactly ([fefb17d](https://github.com/create2-labs/cafe-persistence/commit/fefb17d2fc142dc2deb3ee4e5fa8ef1588a8b84e))
* upgrade to go 1.27.4 to solve GO-2026-6859 ([b5f9dc3](https://github.com/create2-labs/cafe-persistence/commit/b5f9dc3c130fcebe4df4d6b9572fc22a2b6664a5))

## [2.2.0](https://github.com/create2-labs/cafe-persistence/compare/v2.1.0...v2.2.0) (2026-11-02)


### Features

* add nullable transition_id on wallet scans ([148cd2d](https://github.com/create2-labs/cafe-persistence/commit/148cd2db6845aba7a4ff65c01a470d5c688a3404))
* add nullable transition_id on wallet scans ([2dc50a5](https://github.com/create2-labs/cafe-persistence/commit/2dc50a5cd6b9d7590b5f5651daf67d64f04e230f))
* persist wallet public_key_recovery ([6c9967f](https://github.com/create2-labs/cafe-persistence/commit/6c9967fc89c9575333e9f04286b54ffcebf3ccf2))
* persist wallet public_key_recovery ([7be06b7](https://github.com/create2-labs/cafe-persistence/commit/7be06b7d17027c238965ac67aa773144f0fc054c))
* publish wallet.observed with the contracts subject ([7ef7765](https://github.com/create2-labs/cafe-persistence/commit/7ef7765556d0073b6bc7b1b25e02bdb49487388b))
* publish wallet.observed with the contracts subject ([3e95cd4](https://github.com/create2-labs/cafe-persistence/commit/3e95cd4612af5a34930c3e164c2d886a10087096))
* **scan:** apply a wallet transition transaction ([ae31904](https://github.com/create2-labs/cafe-persistence/commit/ae319048c6804b6d149c360e61ac4c7069ad4dc1))
* **scan:** apply a wallet transition transaction ([2f9bf4d](https://github.com/create2-labs/cafe-persistence/commit/2f9bf4d94c084a6154c481b11cb9f2153fc78a58))
* **scan:** document the wallet transition route ([3dd7553](https://github.com/create2-labs/cafe-persistence/commit/3dd7553f6e780918ee6d369f9f3e5623ae96ada3))
* **scan:** document the wallet transition route ([91ecd25](https://github.com/create2-labs/cafe-persistence/commit/91ecd25a3345bc652e2f1e9171b3ba39e4538cb6))
* **scan:** project a terminal wallet transition ([ce9b342](https://github.com/create2-labs/cafe-persistence/commit/ce9b342ef7f5d67b1ff221c9f353e01f493e58db))
* **scan:** project a terminal wallet transition ([7444784](https://github.com/create2-labs/cafe-persistence/commit/7444784528cff22e25a1948ee799f067245d96db))
* **scan:** serve wallet transition outcomes ([183b110](https://github.com/create2-labs/cafe-persistence/commit/183b110ab4bb27eac7dd5227e8c4c23094426fe0))
* **scan:** serve wallet transition outcomes ([3058b0a](https://github.com/create2-labs/cafe-persistence/commit/3058b0af1204281523f1cb2357e98ed7d2d19eb8))
* **scan:** stop committing a wallet scan from NATS ([1c41ff6](https://github.com/create2-labs/cafe-persistence/commit/1c41ff6c0d74e0052326d99953625497dd8b0f34))
* **scan:** stop committing a wallet scan from NATS ([c41ee9c](https://github.com/create2-labs/cafe-persistence/commit/c41ee9cf058802eaa7a887e52857ed24d195f9bc))
* **scan:** stop wallet NATS projections and failure writes ([0dae82f](https://github.com/create2-labs/cafe-persistence/commit/0dae82f222c7b2538902e71e871125785ee87850))
* **scan:** stop wallet NATS projections and failure writes ([15464db](https://github.com/create2-labs/cafe-persistence/commit/15464db1b024a0efaf394f78ce04400a98664a19))


### Bug Fixes

* scan with Go 1.26.9 so govulncheck clears the stdlib ([a279c09](https://github.com/create2-labs/cafe-persistence/commit/a279c097aac317b8fbcfab64a9a7cabf6335f3e8))
* scan with Go 1.27.2 so govulncheck clears the stdlib ([da5b191](https://github.com/create2-labs/cafe-persistence/commit/da5b1913913a8fe39ef9b48ff9308f588e5c470a))
* scan with Go 1.27.2 so govulncheck clears the stdlib ([ae2bccd](https://github.com/create2-labs/cafe-persistence/commit/ae2bccd4002a28d0ad158492bae0c41e0277f645))
* upgrade to Go 1.27.3 ([9fc94d9](https://github.com/create2-labs/cafe-persistence/commit/9fc94d950a43dc42f7fb1443dac5cd9f7681edc1))
* upgrade to Go 1.27.3 ([034b22c](https://github.com/create2-labs/cafe-persistence/commit/034b22c0a46c2217a8aa0a98c6f224ff828163e5))
