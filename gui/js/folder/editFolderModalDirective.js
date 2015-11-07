angular.module('syncthingfuse.folder')
    .directive('editFolderModal', function () {
        return {
            restrict: 'A',
            templateUrl: 'js/folder/editFolderModalView.html'
        };
});

angular.module('syncthingfuse.folder')
    .directive('humansize', function($q, $http) {
        return {
            require: 'ngModel',
            link: function(scope, elm, attrs, ctrl) {
                ctrl.$asyncValidators.humansize = function(modelValue, viewValue) {

                    if (ctrl.$isEmpty(modelValue)) {
                        // consider empty model valid
                        return $q.when();
                    }

                    var def = $q.defer();

                    $http.post('/api/verify/humansize', modelValue).then(
                        function() {
                            def.resolve();
                        },
                        function() {
                            def.reject();
                        });

                    return def.promise;
                };
            }
        };
    });
