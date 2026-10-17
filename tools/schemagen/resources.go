package main

// Resources lists the objects the generator writes tables for. A resource joins the list
// when its declaration is written.
var Resources = []Resource{
	{Package: "product", CreatePath: "/v1/products", UpdatePath: "/v1/products/{id}"},
}
