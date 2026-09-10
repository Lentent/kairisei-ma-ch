package cnbootstrap

import "kairisei.local/server/internal/release"

func cnLocalShopProducts() []map[string]any {
	products := release.LocalShopProducts()
	result := make([]map[string]any, 0, len(products))
	for _, product := range products {
		result = append(result, map[string]any{
			"bid": product.ID, "name": product.Name, "price": "0", "gold": product.Crystal,
			"pids": map[string]string{}, "ratios": map[string]string{"local": "1"},
		})
	}
	return result
}
