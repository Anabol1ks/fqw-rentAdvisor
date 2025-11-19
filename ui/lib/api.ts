const API_BASE_URL = process.env.NEXT_PUBLIC_API_BASE_URL || 'http://localhost:8080';

export interface AddressValuationRequest {
  address: string;
  city: string;
  rooms: number;
  area_total: number;
  area_living?: number;
  area_kitchen?: number;
  floor?: number;
  floors_total?: number;
  year_built?: number;
  house_material?: string;
  condition?: string;
  with_text?: boolean;
}

export interface AddressValuationResponse {
  report_id: string;
  object: {
    address: string;
    city: string;
    rooms: number;
    area_total: number;
    floor?: number;
    floors_total?: number;
    year_built?: number;
    house_material?: string;
  };
  price: {
    currency: string;
    deal_type: string;
    prediction_rub: number;
    interval_low_rub: number;
    interval_high_rub: number;
  };
  text?: {
    summary_short: string;
    summary_long: string;
    factors_summary: string[];
  };
  comparables: Array<{
    rooms: number;
    area_total: number;
    price_rub: number;
    distance_km: number;
    metro_station: string;
    url: string;
  }>;
}

export interface ValuationListItem {
  id: string;
  address: string;
  city: string;
  deal_type: string;
  prediction_rub: number;
  interval_low_rub: number;
  interval_high_rub: number;
  summary_short?: string;
  created_at: string;
}

export interface ValuationListResponse {
  items: ValuationListItem[];
  limit: number;
  offset: number;
}

export const api = {
  async checkMLHealth(): Promise<boolean> {
    try {
      const response = await fetch(
				`${API_BASE_URL}/api/v1/valuation/ml-health`,
				{
					method: 'GET',
					headers: {
						'Content-Type': 'application/json',
					},
				}
			)
      return response.ok;
    } catch (error) {
      console.error('ML health check failed:', error);
      return false;
    }
  },

  async createValuation(data: AddressValuationRequest): Promise<AddressValuationResponse> {
    const response = await fetch(`${API_BASE_URL}/api/v1/valuation/address`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify(data),
    });

    if (!response.ok) {
      const error = await response.json();
      throw new Error(error.error || 'Ошибка при создании оценки');
    }

    return response.json();
  },

  async getValuationList(limit: number = 20, offset: number = 0): Promise<ValuationListResponse> {
    const response = await fetch(
      `${API_BASE_URL}/api/v1/valuation?limit=${limit}&offset=${offset}`,
      {
        method: 'GET',
        headers: {
          'Content-Type': 'application/json',
        },
      }
    );

    if (!response.ok) {
      const error = await response.json();
      throw new Error(error.error || 'Ошибка при получении списка');
    }

    return response.json();
  },

  async getValuationById(id: string): Promise<AddressValuationResponse> {
    const response = await fetch(`${API_BASE_URL}/api/v1/valuation/${id}`, {
      method: 'GET',
      headers: {
        'Content-Type': 'application/json',
      },
    });

    if (!response.ok) {
      const error = await response.json();
      throw new Error(error.error || 'Ошибка при получении отчета');
    }

    return response.json();
  },

  getValuationPdfUrl(id: string): string {
    return `${API_BASE_URL}/api/v1/valuation/${id}/pdf`;
  },
};
