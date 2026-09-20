package cnbootstrap

import (
	"path/filepath"

	"kairisei.local/server/internal/cpk"
)

// clientResources is a validated, read-only inventory shared by every account.
type clientResources struct {
	GachaBanner         string
	FiveStarGachaBanner string
	HomeBanner          string
	GachaBanners        map[string]string
	HomeEventBanner     string
	CPK                 []cpk.File
	Images              cardImages
	CPKList             []byte
	CPKState            []byte
	PatchRoots          []string
	Catalog             []byte
	Patch               []cnPatchDelivery
	AvailableBundles    map[string]struct{}
}

func loadClientResources(config ResourcesConfig) (*clientResources, error) {
	images, err := loadCardImages(config.ImageRoot)
	if err != nil {
		return nil, err
	}
	absoluteGachaBanner, err := resolveBanner(config.GachaBanner, "gacha")
	if err != nil {
		return nil, err
	}
	absoluteFiveStarGachaBanner, err := resolveBanner(config.FiveStarGachaBanner, "five-star gacha")
	if err != nil {
		return nil, err
	}
	absoluteHomeBanner, err := resolveBanner(config.HomeBanner, "Home")
	if err != nil {
		return nil, err
	}
	homeEventBanner, err := resolveBanner(filepath.Join(filepath.Dir(absoluteHomeBanner), cn602HomeEventBannerFile), "Home event")
	if err != nil {
		return nil, err
	}
	gachaBannerPaths, err := resolveCN602GachaBannerPaths(absoluteGachaBanner, absoluteFiveStarGachaBanner)
	if err != nil {
		return nil, err
	}
	cpkAliases, err := cpk.LoadAliases(config.CPKRoot, config.CPKAliases)
	if err != nil {
		return nil, err
	}
	cpkDelivery, err := cpk.Load(config.CPKRoot, cpkAliases)
	if err != nil {
		return nil, err
	}
	cpkFileList := cpk.FileList(cpkDelivery)
	cpkPatchState := cpk.PatchState(cpkDelivery)
	resolvedPatchRoots, err := resolveCNPatchRoots(config.PatchRoots)
	if err != nil {
		return nil, err
	}
	catalogContent, err := buildCN602Catalog(resolvedPatchRoots, config.AssetMap)
	if err != nil {
		return nil, err
	}
	patchDelivery, err := cnPatchDeliveryFromCatalog(catalogContent)
	if err != nil {
		return nil, err
	}
	availableBundles, err := loadCNAssetMapBundleSet(config.AssetMap)
	if err != nil {
		return nil, err
	}
	return &clientResources{
		GachaBanner:         absoluteGachaBanner,
		FiveStarGachaBanner: absoluteFiveStarGachaBanner,
		HomeBanner:          absoluteHomeBanner,
		GachaBanners:        gachaBannerPaths,
		HomeEventBanner:     homeEventBanner,
		CPK:                 cpkDelivery,
		Images:              images,
		CPKList:             cpkFileList,
		CPKState:            cpkPatchState,
		PatchRoots:          resolvedPatchRoots,
		Catalog:             catalogContent,
		Patch:               patchDelivery,
		AvailableBundles:    availableBundles,
	}, nil
}
